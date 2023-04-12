# Kube logs wrapper API

Tiny API to fecth logs in streamming fashion from Kubernetes API

```bash
docker image build --tag juliocesarmidia/kube-log-wrapper-api:latest .


-N, --no-buffer     Disable buffering of the output stream
-s, --silent        Silent mode

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=saquedigital&namespace=superdigital&tailLines=1000"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=saquedigital&namespace=superdigital"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=pix-payment-gcrr-cronjob&namespace=superdigital"


curl -N --silent "http://localhost:9000/v1/logs?selectorKey=app&selectorValue=pod-metrics&namespace=amazon-cloudwatch&tailLines=1000"


curl -N --silent "http://localhost:9000/v1/logs?selectorKey=k8s-app&selectorValue=kube-dns&namespace=kube-system&tailLines=1000"

curl -N --silent "http://localhost:9000/v1/logs?selectorKey=component&selectorValue=etcd&namespace=kube-system&tailLines=1000"
```
