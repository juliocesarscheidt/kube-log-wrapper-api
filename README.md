# Kube logs wrapper API

Tiny API made with Golang, to fetch logs as streaming from Kubernetes API

```bash
export X_API_KEY='$(cat /dev/urandom | tr -dc 'a-zA-Z0-9!@#$%&*(){}[]+-' | fold -w 32 | head -n 1)'

# run locally
RUNNING_IN_KUBERNETES=0 go run main.go

# run with docker
docker image build --tag docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0 .

docker image push docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0

docker container run --rm -d \
  --name kube-log-wrapper-api \
  --env RUNNING_IN_KUBERNETES="0" \
  --env KUBECONFIG="/tmp/config" \
  --env X_API_KEY \
  --volume $HOME/.kube/config:/tmp/config:ro \
  --network host \
  docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0

docker container logs -f --tail 100 kube-log-wrapper-api

docker container rm -f kube-log-wrapper-api

# run with kubernetes
# create a secret containing the X_API_KEY
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: kube-log-wrapper-api-secrets
  namespace: default
  labels:
    app.kubernetes.io/name: kube-log-wrapper-api
data:
  X_API_KEY: "$(echo -n "$X_API_KEY" | base64 -w0)"
EOF

# create the deployment
kubectl apply -f deployment.yaml

kubectl get deploy,svc,sa,secret -l app.kubernetes.io/name=kube-log-wrapper-api -n default

kubectl get pod -l app.kubernetes.io/name=kube-log-wrapper-api -n default
kubectl top pod -l app.kubernetes.io/name=kube-log-wrapper-api -n default

kubectl logs -f -l app.kubernetes.io/name=kube-log-wrapper-api -n default --tail 100 --timestamps

kubectl exec -it pod/kube-log-wrapper-api-845f95b74-zwftz -n default -- sh
ls /var/run/secrets/kubernetes.io/serviceaccount/


SVC_IP=$(kubectl get service -n default \
  -l app.kubernetes.io/name=kube-log-wrapper-api --no-headers \
  | tr -s ' ' ' ' | cut -d' ' -f3)

curl -N -s --url "http://$SVC_IP/v1/health"

curl -H "Authorization: X-Api-Key $X_API_KEY" -N -s --url "http://$SVC_IP/v1/logs?selectorKey=k8s-app&selectorValue=metrics-server&namespace=kube-system&tailLines=10"

# clean up
kubectl delete -f deployment.yaml
kubectl delete secret kube-log-wrapper-api-secrets -n default


# calling API
# -N, --no-buffer     Disable buffering of the output stream
# -s, --silent        Silent mode

curl -N -s --url "http://localhost:9000/v1/health"

curl -H "Authorization: X-Api-Key $X_API_KEY" -N -s --url "http://localhost:9000/v1/logs?selectorKey=k8s-app&selectorValue=metrics-server&namespace=kube-system&tailLines=10"

curl -H "Authorization: X-Api-Key $X_API_KEY" -N -s --url "http://localhost:9000/v1/logs?selectorKey=k8s-app&selectorValue=kube-dns&namespace=kube-system&tailLines=10"

curl -H "Authorization: X-Api-Key $X_API_KEY" -N -s --url "http://localhost:9000/v1/logs?selectorKey=component&selectorValue=etcd&namespace=kube-system&tailLines=10"
```
