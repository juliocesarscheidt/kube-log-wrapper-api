# Kube logs wrapper API

Tiny API to fecth logs in streamming fashion from Kubernetes API made with Golang

```bash
docker image build --tag docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0 .

docker image push docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0

docker container run --rm \
  --name kube-log-wrapper-api \
  --env RUNNING_IN_KUBERNETES="0" \
  --env KUBECONFIG="/tmp/config" \
  --volume $HOME/.kube/config:/tmp/config:ro \
  --publish 9000:9000 \
  docker.io/juliocesarmidia/kube-log-wrapper-api:v1.0.0

docker container logs -f --tail 100 kube-log-wrapper-api


# create the deployment
kubectl apply -f deployment.yaml

kubectl get deploy,svc,sa -l app.kubernetes.io/name=kube-log-wrapper-api -n default

kubectl get pod -l app.kubernetes.io/name=kube-log-wrapper-api -n default

kubectl logs -f -l app.kubernetes.io/name=kube-log-wrapper-api -n default --tail 100 --timestamps

kubectl exec -it pod/kube-log-wrapper-api-5cd57c8547-zzbgs -n default -- sh
ls /var/run/secrets/kubernetes.io/serviceaccount/

kubectl delete -f deployment.yaml



RUNNING_IN_KUBERNETES=0 go run main.go
RUNNING_IN_KUBERNETES=1 go run main.go


curl -N --silent "http://localhost:9000/v1/health"


-N, --no-buffer     Disable buffering of the output stream
-s, --silent        Silent mode

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=saquedigital&namespace=superdigital&tailLines=1000"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=saquedigital&namespace=superdigital"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=pix-payment-gcrr-cronjob&namespace=superdigital"


curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=pod-metrics&namespace=amazon-cloudwatch&tailLines=1000"


curl -N --silent "http://localhost:9000/v1/logs?selectorKey=k8s-app&selectorValue=kube-dns&namespace=kube-system&tailLines=1000"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=component&selectorValue=etcd&namespace=kube-system&tailLines=1000"
```
