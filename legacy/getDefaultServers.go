package main

import (
	"fmt"
)

// getBackendServers returns a list of Upstream and Server to be used by the
// backend.  An upstream can be used in multiple servers if the namespace,
// service name and port are the same.
//
//nolint:gocyclo // Ignore function complexity error
func (n *NGINXController) getBackendServers(ingresses []*Ingress) ([]*Backend, []*Server) {
	du := n.getDefaultUpstream()
	upstreams := n.createUpstreams(ingresses, du)
	servers := n.createServers(ingresses, upstreams, du)

	var canaryIngresses []*Ingress

	for _, ing := range ingresses {
		ingKey := k8s.MetaNamespaceKey(ing)
		anns := ing.ParsedAnnotations

		if !n.store.GetBackendConfiguration().AllowSnippetAnnotations {
			dropSnippetDirectives(anns, ingKey)
		}

		for _, rule := range ing.Spec.Rules {
			host := rule.Host
			if host == "" {
				host = defServerName
			}

			server := servers[host]
			if server == nil {
				server = servers[defServerName]
			}

			if rule.HTTP == nil &&
				host != defServerName {
				klog.V(3).Infof("Ingress %q does not contain any HTTP rule, using default backend", ingKey)
				continue
			}

			if server.AuthTLSError == "" && anns.CertificateAuth.AuthTLSError != "" {
				server.AuthTLSError = anns.CertificateAuth.AuthTLSError
			}

			if server.CertificateAuth.CAFileName == "" {
				server.CertificateAuth = anns.CertificateAuth
				if server.CertificateAuth.Secret != "" && server.CertificateAuth.CAFileName == "" {
					klog.V(3).Infof("Secret %q has no 'ca.crt' key, mutual authentication disabled for Ingress %q",
						server.CertificateAuth.Secret, ingKey)
				}
			} else {
				klog.V(3).Infof("Server %q is already configured for mutual authentication (Ingress %q)",
					server.Hostname, ingKey)
			}

			if !n.store.GetBackendConfiguration().ProxySSLLocationOnly {
				if server.ProxySSL.CAFileName == "" {
					server.ProxySSL = anns.ProxySSL
					if server.ProxySSL.Secret != "" && server.ProxySSL.CAFileName == "" {
						klog.V(3).Infof("Secret %q has no 'ca.crt' key, client cert authentication disabled for Ingress %q",
							server.ProxySSL.Secret, ingKey)
					}
				} else {
					klog.V(3).Infof("Server %q is already configured for client cert authentication (Ingress %q)",
						server.Hostname, ingKey)
				}
			}

			if rule.HTTP == nil {
				klog.V(3).Infof("Ingress %q does not contain any HTTP rule, using default backend", ingKey)
				continue
			}

			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service == nil {
					// skip non-service backends
					klog.V(3).Infof("Ingress %q and path %q does not contain a service backend, using default backend", ingKey, path.Path)
					continue
				}

				upsName := upstreamName(ing.Namespace, path.Backend.Service)

				ups := upstreams[upsName]

				// Backend is not referenced to by a server
				if ups.NoServer {
					continue
				}

				nginxPath := rootLocation
				if path.Path != "" {
					nginxPath = path.Path
				}

				addLoc := true
				for _, loc := range server.Locations {
					if loc.Path != nginxPath {
						continue
					}

					// Same paths but different types are allowed
					// (same type means overlap in the path definition)
					if !apiequality.Semantic.DeepEqual(loc.PathType, path.PathType) {
						break
					}

					addLoc = false

					if !loc.IsDefBackend {
						klog.V(3).Infof("Location %q already configured for server %q with upstream %q (Ingress %q)",
							loc.Path, server.Hostname, loc.Backend, ingKey)
						break
					}

					klog.V(3).Infof("Replacing location %q for server %q with upstream %q to use upstream %q (Ingress %q)",
						loc.Path, server.Hostname, loc.Backend, ups.Name, ingKey)

					loc.Backend = ups.Name
					loc.IsDefBackend = false
					loc.Port = ups.Port
					loc.Service = ups.Service
					loc.Ingress = ing

					locationApplyAnnotations(loc, anns)

					if loc.Redirect.FromToWWW {
						server.RedirectFromToWWW = true
					}

					break
				}

				// new location
				if addLoc {
					klog.V(3).Infof("Adding location %q for server %q with upstream %q (Ingress %q)",
						nginxPath, server.Hostname, ups.Name, ingKey)
					loc := &Location{
						Path:         nginxPath,
						PathType:     path.PathType,
						Backend:      ups.Name,
						IsDefBackend: false,
						Service:      ups.Service,
						Port:         ups.Port,
						Ingress:      ing,
					}
					locationApplyAnnotations(loc, anns)

					if loc.Redirect.FromToWWW {
						server.RedirectFromToWWW = true
					}
					server.Locations = append(server.Locations, loc)
				}

				if ups.SessionAffinity.AffinityType == "" {
					ups.SessionAffinity.AffinityType = anns.SessionAffinity.Type
				}

				if ups.SessionAffinity.AffinityMode == "" {
					ups.SessionAffinity.AffinityMode = anns.SessionAffinity.Mode
				}

				if anns.SessionAffinity.Type == "cookie" {
					cookiePath := anns.SessionAffinity.Cookie.Path
					if anns.Rewrite.UseRegex && cookiePath == "" {
						klog.Warningf("session-cookie-path should be set when use-regex is true")
					}

					ups.SessionAffinity.CookieSessionAffinity.Name = anns.SessionAffinity.Cookie.Name
					ups.SessionAffinity.CookieSessionAffinity.Expires = anns.SessionAffinity.Cookie.Expires
					ups.SessionAffinity.CookieSessionAffinity.MaxAge = anns.SessionAffinity.Cookie.MaxAge
					ups.SessionAffinity.CookieSessionAffinity.Secure = anns.SessionAffinity.Cookie.Secure
					ups.SessionAffinity.CookieSessionAffinity.Path = cookiePath
					ups.SessionAffinity.CookieSessionAffinity.Domain = anns.SessionAffinity.Cookie.Domain
					ups.SessionAffinity.CookieSessionAffinity.SameSite = anns.SessionAffinity.Cookie.SameSite
					ups.SessionAffinity.CookieSessionAffinity.ConditionalSameSiteNone = anns.SessionAffinity.Cookie.ConditionalSameSiteNone
					ups.SessionAffinity.CookieSessionAffinity.ChangeOnFailure = anns.SessionAffinity.Cookie.ChangeOnFailure

					locs := ups.SessionAffinity.CookieSessionAffinity.Locations
					if _, ok := locs[host]; !ok {
						locs[host] = []string{}
					}
					locs[host] = append(locs[host], path.Path)

					if len(server.Aliases) > 0 {
						for _, alias := range server.Aliases {
							if _, ok := locs[alias]; !ok {
								locs[alias] = []string{}
							}
							locs[alias] = append(locs[alias], path.Path)
						}
					}
				}
			}
		}

		// set aside canary ingresses to merge later
		if anns.Canary.Enabled {
			canaryIngresses = append(canaryIngresses, ing)
		}
	}

	if nonCanaryIngressExists(ingresses, canaryIngresses) {
		for _, canaryIng := range canaryIngresses {
			mergeAlternativeBackends(canaryIng, upstreams, servers)
		}
	}

	aUpstreams := make([]*Backend, 0, len(upstreams))

	for _, upstream := range upstreams {
		aUpstreams = append(aUpstreams, upstream)

		if upstream.Name == defUpstreamName {
			continue
		}

		isHTTPSfrom := []*Server{}
		for _, server := range servers {
			for _, location := range server.Locations {
				// use default backend
				if !shouldCreateUpstreamForLocationDefaultBackend(upstream, location) {
					continue
				}

				if len(location.DefaultBackend.Spec.Ports) == 0 {
					klog.Errorf("Custom default backend service %v/%v has no ports. Ignoring", location.DefaultBackend.Namespace, location.DefaultBackend.Name)
					continue
				}

				sp := location.DefaultBackend.Spec.Ports[0]
				var zone string
				if n.cfg.EnableTopologyAwareRouting {
					zone = getIngressPodZone(location.DefaultBackend)
				} else {
					zone = emptyZone
				}
				endps := getEndpointsFromSlices(location.DefaultBackend, &sp, apiv1.ProtocolTCP, zone, n.store.GetServiceEndpointsSlices)
				// custom backend is valid only if contains at least one endpoint
				if len(endps) > 0 {
					name := fmt.Sprintf("custom-default-backend-%v-%v", location.DefaultBackend.GetNamespace(), location.DefaultBackend.GetName())
					klog.V(3).Infof("Creating \"%v\" upstream based on default backend annotation", name)

					nb := upstream.DeepCopy()
					nb.Name = name
					nb.Endpoints = endps
					aUpstreams = append(aUpstreams, nb)
					location.DefaultBackendUpstreamName = name

					if len(upstream.Endpoints) == 0 {
						klog.V(3).Infof("Upstream %q has no active Endpoint, so using custom default backend for location %q in server %q (Service \"%v/%v\")",
							upstream.Name, location.Path, server.Hostname, location.DefaultBackend.Namespace, location.DefaultBackend.Name)

						location.Backend = name
					}
				}

				if server.SSLPassthrough {
					if location.Path == rootLocation {
						if location.Backend == defUpstreamName {
							klog.Warningf("Server %q has no default backend, ignoring SSL Passthrough.", server.Hostname)
							continue
						}
						isHTTPSfrom = append(isHTTPSfrom, server)
					}
				}
			}
		}

		if len(isHTTPSfrom) > 0 {
			upstream.SSLPassthrough = true
		}
	}

	aServers := make([]*Server, 0, len(servers))
	for _, value := range servers {
		sort.SliceStable(value.Locations, func(i, j int) bool {
			return value.Locations[i].Path > value.Locations[j].Path
		})

		sort.SliceStable(value.Locations, func(i, j int) bool {
			return len(value.Locations[i].Path) > len(value.Locations[j].Path)
		})
		aServers = append(aServers, value)
	}

	sort.SliceStable(aUpstreams, func(a, b int) bool {
		return aUpstreams[a].Name < aUpstreams[b].Name
	})

	sort.SliceStable(aServers, func(i, j int) bool {
		return aServers[i].Hostname < aServers[j].Hostname
	})

	return aUpstreams, aServers
}

// getDefaultUpstream returns the upstream associated with the default backend.
// Configures the upstream to return HTTP code 503 in case of error.
func (n *NGINXController) getDefaultUpstream() *Backend {
	upstream := &Backend{
		Name: defUpstreamName,
	}
	svcKey := n.cfg.DefaultService

	if svcKey == "" {
		upstream.Endpoints = append(upstream.Endpoints, n.DefaultEndpoint())
		return upstream
	}

	svc, err := n.store.GetService(svcKey)
	if err != nil {
		klog.Warningf("Error getting default backend %q: %v", svcKey, err)
		upstream.Endpoints = append(upstream.Endpoints, n.DefaultEndpoint())
		return upstream
	}
	var zone string
	if n.cfg.EnableTopologyAwareRouting {
		zone = getIngressPodZone(svc)
	} else {
		zone = emptyZone
	}
	endps := getEndpointsFromSlices(svc, &svc.Spec.Ports[0], apiv1.ProtocolTCP, zone, n.store.GetServiceEndpointsSlices)
	if len(endps) == 0 {
		klog.Warningf("Service %q does not have any active Endpoint", svcKey)
		endps = []Endpoint{n.DefaultEndpoint()}
	}

	upstream.Service = svc
	upstream.Endpoints = append(upstream.Endpoints, endps...)
	return upstream
}
