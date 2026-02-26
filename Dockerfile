# Build stage: compile the patched controller binary from ingress-nginx source
ARG INGRESS_NGINX_IMAGE=registry.k8s.io/ingress-nginx/controller:v1.14.3
FROM golang:1.25.6-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /src

# Clone the exact ingress-nginx version and apply the patch
ARG INGRESS_NGINX_VERSION=controller-v1.14.3
RUN git clone --depth 1 --branch ${INGRESS_NGINX_VERSION} \
    https://github.com/kubernetes/ingress-nginx.git .

# Apply the patch to re-enable nginx -t in admission webhook
COPY patch/re-enable-nginx-test.patch /tmp/
RUN git apply /tmp/re-enable-nginx-test.patch

# Download dependencies and build
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w \
      -X k8s.io/ingress-nginx/version.RELEASE=${INGRESS_NGINX_VERSION}-validator \
      -X k8s.io/ingress-nginx/version.COMMIT=$(git rev-parse --short HEAD) \
      -X k8s.io/ingress-nginx/version.REPO=https://github.com/kubernetes/ingress-nginx" \
    -o /nginx-ingress-controller \
    k8s.io/ingress-nginx/cmd/nginx

# Final stage: overlay patched binary onto the stock controller image
ARG INGRESS_NGINX_IMAGE
FROM ${INGRESS_NGINX_IMAGE}

USER root
COPY --from=builder /nginx-ingress-controller /nginx-ingress-controller
USER www-data
