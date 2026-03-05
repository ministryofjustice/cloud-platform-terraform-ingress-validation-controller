package main

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	rootLocation = "/"
)

var (
	pathTypeExact  = networking.PathTypeExact
	pathTypePrefix = networking.PathTypePrefix
)

// getConfiguration returns the configuration matching the standard kubernetes ingress
func (n *NGINXController) getConfiguration(ingresses []*Ingress) (sets.Set[string], []*Server, *Configuration) {
	upstreams, servers := n.getBackendServers(ingresses)
	var passUpstreams []*SSLPassthroughBackend

	hosts := sets.New[string]()

	for _, server := range servers {
		// If a location is defined by a prefix string that ends with the slash character, and requests are processed by one of
		// proxy_pass, fastcgi_pass, uwsgi_pass, scgi_pass, memcached_pass, or grpc_pass, then the special processing is performed.
		// In response to a request with URI equal to // this string, but without the trailing slash, a permanent redirect with the
		// code 301 will be returned to the requested URI with the slash appended. If this is not desired, an exact match of the
		// URIand location could be defined like this:
		//
		// location /user/ {
		//     proxy_pass http://user.example.com;
		// }
		// location = /user {
		//     proxy_pass http://login.example.com;
		// }
		server.Locations = updateServerLocations(server.Locations)

		if !hosts.Has(server.Hostname) {
			hosts.Insert(server.Hostname)
		}

		for _, alias := range server.Aliases {
			if !hosts.Has(alias) {
				hosts.Insert(alias)
			}
		}

		if !server.SSLPassthrough {
			continue
		}

		for _, loc := range server.Locations {
			if loc.Path != rootLocation {
				log.Println("Ignoring SSL Passthrough for location %q in server %q", loc.Path, server.Hostname)
				continue
			}
			passUpstreams = append(passUpstreams, &SSLPassthroughBackend{
				Backend:  loc.Backend,
				Hostname: server.Hostname,
				Service:  loc.Service,
				Port:     loc.Port,
			})
			break
		}
	}

	return hosts, servers, &Configuration{
		Backends:              upstreams,
		Servers:               servers,
		TCPEndpoints:          n.getStreamServices(n.cfg.TCPConfigMapName, apiv1.ProtocolTCP),
		UDPEndpoints:          n.getStreamServices(n.cfg.UDPConfigMapName, apiv1.ProtocolUDP),
		PassthroughBackends:   passUpstreams,
		BackendConfigChecksum: n.store.GetBackendConfiguration().Checksum,
		DefaultSSLCertificate: n.getDefaultSSLCertificate(),
		StreamSnippets:        n.getStreamSnippets(ingresses),
	}
}

// updateServerLocations inspects the generated locations configuration for a server
// normalizing the path and adding an additional exact location when is possible
func updateServerLocations(locations []*Location) []*Location {
	newLocations := []*Location{}

	// get Exact locations to check if one already exists
	exactLocations := map[string]*Location{}
	for _, location := range locations {
		if *location.PathType == pathTypeExact {
			exactLocations[location.Path] = location
		}
	}

	for _, location := range locations {
		// location / does not require any update
		if location.Path == rootLocation {
			newLocations = append(newLocations, location)
			continue
		}

		location.IngressPath = location.Path

		// only Prefix locations could require an additional location block
		if *location.PathType != pathTypePrefix {
			newLocations = append(newLocations, location)
			continue
		}

		// locations with rewrite or using regular expressions are not modified
		if needsRewrite(location) || location.Rewrite.UseRegex {
			newLocations = append(newLocations, location)
			continue
		}

		// If exists an Exact location is not possible to create a new one.
		if _, alreadyExists := exactLocations[location.Path]; alreadyExists {
			// normalize path. Must end in /
			location.Path = normalizePrefixPath(location.Path)
			newLocations = append(newLocations, location)
			continue
		}

		var el Location = *location

		// normalize path. Must end in /
		location.Path = normalizePrefixPath(location.Path)
		newLocations = append(newLocations, location)

		// add exact location
		exactLocation := &el
		exactLocation.PathType = &pathTypeExact

		newLocations = append(newLocations, exactLocation)
	}

	return newLocations
}

func normalizePrefixPath(path string) string {
	if path == rootLocation {
		return rootLocation
	}

	if !strings.HasSuffix(path, "/") {
		return fmt.Sprintf("%v/", path)
	}

	return path
}

func needsRewrite(location *Location) bool {
	if location.Rewrite.Target != "" && location.Rewrite.Target != location.Path {
		return true
	}

	return false
}

// Test checks if config file is a syntax valid nginx configuration
func Test(cfg string) ([]byte, error) {
	//nolint:gosec // Ignore G204 error
	return exec.Command("nc.Binary", "-c", cfg, "-t").CombinedOutput() // TODO: use right binary location
}

func (n *NGINXController) getStreamServices(configmapName string, proto apiv1.Protocol) []L4Service {
	if configmapName == "" {
		return []L4Service{}
	}
	log.Println("Obtaining information about %v stream services from ConfigMap %q", proto, configmapName)
	_, _, err := k8s.ParseNameNS(configmapName)
	if err != nil {
		log.Println("Error parsing ConfigMap reference %q: %v", configmapName, err)
		return []L4Service{}
	}
	configmap, err := n.store.GetConfigMap(configmapName)
	if err != nil {
		log.Println("Error getting ConfigMap %q: %v", configmapName, err)
		return []L4Service{}
	}

	svcs := make([]L4Service, 0, len(configmap.Data))
	var svcProxyProtocol ingress.ProxyProtocol

	rp := []int{
		n.cfg.ListenPorts.HTTP,
		n.cfg.ListenPorts.HTTPS,
		n.cfg.ListenPorts.SSLProxy,
		n.cfg.ListenPorts.Health,
		n.cfg.ListenPorts.Default,
		nginx.ProfilerPort,
		nginx.StatusPort,
		nginx.StreamPort,
	}

	reservedPorts := sets.NewInt(rp...)
	// svcRef format: <(str)namespace>/<(str)service>:<(intstr)port>[:<("PROXY")decode>:<("PROXY")encode>]
	for port, svcRef := range configmap.Data {
		externalPort, err := strconv.Atoi(port) // #nosec
		if err != nil {
			log.Println("%q is not a valid %v port number", port, proto)
			continue
		}
		if reservedPorts.Has(externalPort) {
			log.Println("Port %d cannot be used for %v stream services. It is reserved for the Ingress controller.", externalPort, proto)
			continue
		}
		nsSvcPort := strings.Split(svcRef, ":")
		if len(nsSvcPort) < 2 {
			log.Println("Invalid Service reference %q for %v port %d", svcRef, proto, externalPort)
			continue
		}
		nsName := nsSvcPort[0]
		svcPort := nsSvcPort[1]
		svcProxyProtocol.Decode = false
		svcProxyProtocol.Encode = false
		// Proxy Protocol is only compatible with TCP Services
		if len(nsSvcPort) >= 3 && proto == apiv1.ProtocolTCP {
			if len(nsSvcPort) >= 3 && strings.EqualFold(nsSvcPort[2], "PROXY") {
				svcProxyProtocol.Decode = true
			}
			if len(nsSvcPort) == 4 && strings.EqualFold(nsSvcPort[3], "PROXY") {
				svcProxyProtocol.Encode = true
			}
		}
		svcNs, svcName, err := k8s.ParseNameNS(nsName)
		if err != nil {
			log.Println("%v", err)
			continue
		}
		svc, err := n.store.GetService(nsName)
		if err != nil {
			log.Println("Error getting Service %q: %v", nsName, err)
			continue
		}
		var endps []Endpoint
		/* #nosec */
		targetPort, err := strconv.Atoi(svcPort) // #nosec
		var zone string
		if n.cfg.EnableTopologyAwareRouting {
			zone = getIngressPodZone(svc)
		} else {
			zone = emptyZone
		}

		if err != nil {
			// not a port number, fall back to using port name
			log.Println("Searching Endpoints with %v port name %q for Service %q", proto, svcPort, nsName)
			for i := range svc.Spec.Ports {
				sp := svc.Spec.Ports[i]
				if sp.Name == svcPort {
					if sp.Protocol == proto {
						endps = getEndpointsFromSlices(svc, &sp, proto, zone, n.store.GetServiceEndpointsSlices)
						break
					}
				}
			}
		} else {
			log.Println("Searching Endpoints with %v port number %d for Service %q", proto, targetPort, nsName)
			for i := range svc.Spec.Ports {
				sp := svc.Spec.Ports[i]
				//nolint:gosec // Ignore G109 error
				if sp.Port == int32(targetPort) {
					if sp.Protocol == proto {
						endps = getEndpointsFromSlices(svc, &sp, proto, zone, n.store.GetServiceEndpointsSlices)
						break
					}
				}
			}
		}
		// stream services cannot contain empty upstreams and there is
		// no default backend equivalent
		if len(endps) == 0 {
			log.Println("Service %q does not have any active Endpoint for %v port %v", nsName, proto, svcPort)
			continue
		}
		svcs = append(svcs, L4Service{
			Port: externalPort,
			Backend: L4Backend{
				Name:          svcName,
				Namespace:     svcNs,
				Port:          intstr.FromString(svcPort),
				Protocol:      proto,
				ProxyProtocol: svcProxyProtocol,
			},
			Endpoints: endps,
			Service:   svc,
		})
	}
	// Keep upstream order sorted to reduce unnecessary nginx config reloads.
	sort.SliceStable(svcs, func(i, j int) bool {
		return svcs[i].Port < svcs[j].Port
	})
	return svcs
}

func (n *NGINXController) getDefaultSSLCertificate() *SSLCert {
	// read custom default SSL certificate, fall back to generated default certificate
	if n.cfg.DefaultSSLCertificate != "" {
		certificate, err := n.store.GetLocalSSLCert(n.cfg.DefaultSSLCertificate)
		if err == nil {
			return certificate
		}

		log.Println("Error loading custom default certificate, falling back to generated default:\n%v", err)
	}

	return n.cfg.FakeCertificate
}

func (n *NGINXController) getStreamSnippets(ingresses []*Ingress) []string {
	snippets := make([]string, 0, len(ingresses))
	for _, i := range ingresses {
		if i.ParsedAnnotations.StreamSnippet == "" {
			continue
		}
		snippets = append(snippets, i.ParsedAnnotations.StreamSnippet)
	}
	return snippets
}
