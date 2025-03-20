launch-cluster:
    @ minikube start \
        --profile=cerbos-spiffe-demo \
        --cpus=4 \
        --disk-size=30g \
        --memory=8g \
        --extra-config=apiserver.service-account-signing-key-file=/var/lib/minikube/certs/sa.key \
        --extra-config=apiserver.service-account-key-file=/var/lib/minikube/certs/sa.pub \
        --extra-config=apiserver.service-account-issuer=api \
        --extra-config=apiserver.api-audiences=api,spire-server \
        --extra-config=apiserver.authorization-mode=Node,RBAC

teardown-cluster:
    @ minikube delete --profile=cerbos-spiffe-demo

setup-istio-spire: install-spire configure-spire-controller install-istio

install-spire:
    @ helm upgrade --install -n spire-server spire-crds spire-crds --repo https://spiffe.github.io/helm-charts-hardened/ --create-namespace
    @ helm upgrade --install -n spire-server spire spire --repo https://spiffe.github.io/helm-charts-hardened/ --wait --set global.spire.trustDomain="cerbos.dev"

configure-spire-controller:
    #!/usr/bin/env bash

    set -euo pipefail

    kubectl apply -f - <<-EOF
    apiVersion: spire.spiffe.io/v1alpha1
    kind: ClusterSPIFFEID
    metadata:
        name: istio-ingressgateway-reg
    spec:
        spiffeIDTemplate: "spiffe://{{{{ .TrustDomain }}/ns/{{{{ .PodMeta.Namespace }}/sa/{{{{ .PodSpec.ServiceAccountName }}"
        workloadSelectorTemplates:
            - "k8s:ns:istio-system"
            - "k8s:sa:istio-ingressgateway-service-account"
    EOF


    kubectl apply -f - <<-EOF
    apiVersion: spire.spiffe.io/v1alpha1
    kind: ClusterSPIFFEID
    metadata:
        name: istio-sidecar-reg
    spec:
        spiffeIDTemplate: "spiffe://{{{{ .TrustDomain }}/ns/{{{{ .PodMeta.Namespace }}/sa/{{{{ .PodSpec.ServiceAccountName }}"
        podSelector:
            matchLabels:
                spiffe.io/spire-managed-identity: "true"
        workloadSelectorTemplates:
            - "k8s:ns:*"
    EOF

install-istio:
    @ istioctl install --skip-confirmation -f deploy/istio.yaml
    @ kubectl label namespace default istio-injection=enabled --overwrite

install-cerbos:
    @ kubectl create namespace cerbos
    @ kubectl label namespace cerbos istio-injection=enabled --overwrite
    @ kubectl create configmap cerbos-policies --from-file=policies -n cerbos
    @ helm upgrade --install -n cerbos cerbos cerbos/cerbos --wait --values=deploy/cerbos.yaml

uninstall-cerbos:
    @ helm delete -n cerbos cerbos
    @ kubectl delete configmap cerbos-policies -n cerbos
    @ kubectl delete namespace cerbos

deploy-demo:
    @ skaffold build -q | skaffold deploy --build-artifacts -

undeploy-demo:
    @ skaffold delete

curl:
    @ kubectl run curl -it --rm --restart=Never \
        --image=curlimages/curl \
        --labels=sidecar.istio.io/inject='true',spiffe.io/spire-managed-identity='true' \
        --annotations=inject.istio.io/templates='sidecar,spire' \
        --command -- /bin/sh

check NAMESPACE='unprivileged':
    #!/usr/bin/env bash
    echo "Running curl from {{NAMESPACE}} namespace"
    POD=$(kubectl get pods -o=jsonpath='{.items[0].metadata.name}' -n {{NAMESPACE}} -l app=curl)
    for DOCID in "doc1" "doc2" "doc3"; do
        echo "-------------------------"
        echo "Accessing $DOCID"
        kubectl exec -i -t "$POD" -n {{NAMESPACE}} -c curl -- curl -i "http://spiffe-demo-server.default.svc.cluster.local:8080/docs/$DOCID"
    done
