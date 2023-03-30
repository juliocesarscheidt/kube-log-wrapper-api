package main

import (
	"bufio"
	"context"
	"fmt"
	"golang.org/x/exp/slices"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var client *kubernetes.Clientset

func init() {
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else {
		kubeconfig = "/root/.kube/config"
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Printf("error %v", err)
		panic(err)
	}
	client, _ = kubernetes.NewForConfig(config)
}

func getValueOrDefault(value interface{}, defaultValue interface{}) interface{} {
	if value == "" {
		return defaultValue
	}
	return value
}

func fetchPodContainerNamesFromSelector(client *kubernetes.Clientset, namespace string, appLabel string) ([]string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: appLabel})
	if err != nil {
		fmt.Printf("error %v\n", err)
		return nil, nil, err
	}

	podNames := []string{}
	containerNames := []string{}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			containerName := container.Name
			if !slices.Contains(containerNames, containerName) &&
				!strings.Contains(containerName, "sidecar") &&
				!strings.Contains(containerName, "istio-proxy") {
				containerNames = append(containerNames, containerName)
			}
		}
		podNames = append(podNames, pod.Name)
	}

	return podNames, containerNames, nil
}

func fetchPodContainerLogs(client *kubernetes.Clientset, logsChannel chan string, namespace string,
	podNames []string, containerName string, tailLines int64) {

	ctx := context.Background()
	podLogOpts := &corev1.PodLogOptions{
		Container:    containerName,
		Follow:       true,
		SinceSeconds: nil,
		Timestamps:   true,
		TailLines:    &tailLines,
	}
	for _, podName := range podNames {
		go func(podName string, logsChannel chan string) {
			fmt.Printf("podName %s\n", podName)
			fmt.Printf("containerName %s\n", containerName)

			req := client.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)
			logs, err := req.Stream(ctx)
			if err != nil {
				fmt.Printf("error %v\n", err)
				panic(err)
			}

			sc := bufio.NewScanner(logs)
			for sc.Scan() {
				logsChannel <- sc.Text()
			}
		}(podName, logsChannel)
	}
}

func processFetchLogs(client *kubernetes.Clientset, logsChannel chan string, namespace string,
	selectorKey string, selectorValue string, tailLines int64) error {

	appLabel := fmt.Sprintf("%s=%s", selectorKey, selectorValue)
	fmt.Println(appLabel)
	podNames, containerNames, err := fetchPodContainerNamesFromSelector(client, namespace, appLabel)
	if err != nil {
		return err
	}
	for _, containerName := range containerNames {
		fetchPodContainerLogs(client, logsChannel, namespace, podNames, containerName, tailLines)
	}
	return nil
}

func logsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		selectorKey := getValueOrDefault(r.FormValue("selector_key"), "app").(string)
		selectorValue := r.FormValue("selector_value")
		if selectorValue == "" {
			http.Error(w, "missing selector value", http.StatusBadRequest)
			return
		}
		namespace := getValueOrDefault(r.FormValue("namespace"), "default").(string)
		tailLines, _ := strconv.Atoi(getValueOrDefault(r.FormValue("tail_lines"), "1000").(string))

		logsChannel := make(chan string)

		err := processFetchLogs(client, logsChannel, namespace, selectorKey, selectorValue, int64(tailLines))
		if err != nil {
			fmt.Printf("error %v\n", err)
			http.Error(w, "failed to retrieve pods", http.StatusInternalServerError)
			return
		}

		writerFlusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "not flushable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

		for text := range logsChannel {
			if _, err := w.Write([]byte(fmt.Sprintf("%s\n", text))); err != nil {
				fmt.Printf("error %v\n", err)
				return
			}
			writerFlusher.Flush()
		}

		close(logsChannel)
	}
}

func main() {
	r := mux.NewRouter().StrictSlash(true)
	r.Use(mux.CORSMethodMiddleware(r))
	r.Path("/v1/logs").HandlerFunc(logsHandler()).Methods(http.MethodGet)

	srv := &http.Server{
		Handler:     r,
		Addr:        "0.0.0.0:9000",
		IdleTimeout: 30 * time.Second,
		// it will be without these timeouts due to streaming nature
		// ReadTimeout: 5 * time.Second,
		// WriteTimeout: 5 * time.Second,
		// ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Println("[INFO] Server listening on 0.0.0.0:9000")
	log.Fatal(srv.ListenAndServe())

	// curl -N -s "http://localhost:9000/v1/logs?selector_key=app&selector_value=saquedigital&namespace=superdigital&tail_lines=1000"
	// curl -N -s "http://localhost:9000/v1/logs?selector_value=saquedigital&namespace=superdigital"

	// curl -N -s "http://localhost:9000/v1/logs?selector_key=app&selector_value=pod-metrics&namespace=amazon-cloudwatch&tail_lines=1000"

	// curl -N -s "http://localhost:9000/v1/logs?selector_key=k8s-app&selector_value=kube-dns&namespace=kube-system&tail_lines=1000"
	// curl -N -s "http://localhost:9000/v1/logs?selector_key=component&selector_value=etcd&namespace=kube-system&tail_lines=1000"
}
