= Cerbos SPIFFE workload identity demo

Demonstrates how to authorize machine-to-machine interactions using Cerbos. Within the context of Cerbos, the nature of the principal (human or non-human) is not particularly relevant as long as they are correctly identified and authenticated. In this example, workload attestation is provided by [Spire](https://spiffe.io/docs/latest/spire-about/) and verified by [Istio](https://istio.io) service mesh. The application obtains the verified SPIFFE ID from the Istio proxy and forwards that identity as the principal in the Cerbos request. The Cerbos policy checks the trust domain of the principal to decide whether to allow the action or not.


== Extracting the SPIFFE ID and sending a check request to Cerbos

The Istio proxy takes care of validating and verifying the workload identity before passing the request on to the application. The identity is added to the `X-Forwarded-Client-Certificate` header for the application to consume. This header consists of multiple key-value pairs separated by a semicolon. The `URI` key holds the [SPIFFE](https://spiffe.io) identifier of the workload and it can be parsed using the SPIFFE SDK.

```go
func getCallerID(r *http.Request) (spiffeid.ID, error) {
	xfcc := r.Header.Get("X-Forwarded-Client-Cert")
	if xfcc == "" {
		return spiffeid.ID{}, errors.New("empty XFCC header")
	}

	for segment := range strings.SplitSeq(xfcc, ";") {
		if strings.HasPrefix(segment, "URI=") {
			uriStr := strings.TrimPrefix(segment, "URI=")
			uri, err := url.Parse(uriStr)
			if err != nil {
				return spiffeid.ID{}, fmt.Errorf("failed to parse XFCC URI: %w", err)
			}

			id, err := spiffeid.FromURI(uri)
			if err != nil {
				return spiffeid.ID{}, fmt.Errorf("failed to parse SPIFFE ID from URI: %w", err)
			}

			return id, nil
		}
	}

	return spiffeid.ID{}, errors.New("unable to obtain SPIFFE ID from request")
}
```

Use the ID as the principal object in the `CheckResources` request to Cerbos.

```go
allowed, err := cerbosClient.IsAllowed(r.Context(),
    cerbos.NewPrincipal(id.String(), "api").WithAttr("trustDomain", id.TrustDomain().Name()),
    cerbos.NewResource("document", docID).WithAttr("category", doc.Category),
    action,
)
```

The Cerbos policy checks the trust domain and document category to decide whether access is allowed.

```yaml
---
apiVersion: api.cerbos.dev/v1
resourcePolicy:
  version: "default"
  resource: document
  rules:
    - actions: ["read"]
      effect: EFFECT_ALLOW
      roles: ["api"]
      condition:
        match:
          all:
            of:
              - expr: |-
                  P.attr.trustDomain == "cerbos.dev"
              - expr: |-
                  R.attr.category in ["public", "internal"]

    - actions: ["read"]
      effect: EFFECT_ALLOW
      roles: ["api"]
      condition:
        match:
          expr: |-
            P.id == "spiffe://cerbos.dev/ns/privileged/sa/curl"
```

== Running the demo

Requires the following tools installed on the machine.

- [Helm](https://helm.sh)
- [Istioctl](https://istio.io/latest/docs/setup/getting-started/#download)
- [Just](https://just.systems/man/en/packages.html)
- [Kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)
- [Minikube](https://minikube.sigs.k8s.io/docs/)
- [Skaffold](https://skaffold.dev)


Start a Minikube cluster.

```sh
just launch-cluster
```

Install Istio with Spire.

```sh
just setup-istio-spire
```

Install Cerbos.

```sh
just install-cerbos
```

Deploy the demo server.

```sh
just deploy-demo
```

Check access from the `unprivileged` namespace. Access to `doc1` is denied by the Cerbos policy.

```sh
just check unprivileged
```

Check access from the `privileged` namespace. All documents are accessible to this workload.

```sh
just check privileged
```

Tear down the cluster.

```sh
just teardown-cluster
```
