package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
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
		kubeconfig = "~/.kube/config"
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Printf("error %v", err)
		panic(err)
	}
	client, _ = kubernetes.NewForConfig(config)
}

func fetchPodNamesFromSelector(client *kubernetes.Clientset, namespace string, appLabel string) ([]string, error) {
	ctx := context.Background()

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: appLabel})
	if err != nil {
		fmt.Printf("error %v\n", err)
		return nil, err
	}
	podNames := []string{}
	for _, pod := range pods.Items {
		podNames = append(podNames, pod.Name)
	}
	return podNames, nil
}

func fetchPodLogs(client *kubernetes.Clientset, logsChannel chan string, namespace string,
	podNames []string, tailLines int64, watch bool) {
	ctx := context.Background()

	podLogOpts := &corev1.PodLogOptions{
		Container:    "",
		Follow:       watch,
		SinceSeconds: nil,
		Timestamps:   true,
		TailLines:    &tailLines,
	}
	for _, podName := range podNames {
		go func(podName string, logsChannel chan string) {
			fmt.Printf("podName %s\n", podName)

			var logs io.ReadCloser
			req := client.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)
			if watch {
				logs, _ = req.Stream(ctx)
			} else {
				raw, _ := req.DoRaw(ctx)
				logs = io.NopCloser(strings.NewReader(string(raw)))
			}
			defer logs.Close()

			sc := bufio.NewScanner(logs)
			for sc.Scan() {
				logsChannel <- sc.Text()
			}
		}(podName, logsChannel)
	}
}

func getValueOrDefault(value interface{}, defaultValue interface{}) interface{} {
	if value == "" {
		return defaultValue
	}
	return value
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
		watch := true

		appLabel := fmt.Sprintf("%s=%s", selectorKey, selectorValue)
		fmt.Println(appLabel)

		podNames, err := fetchPodNamesFromSelector(client, namespace, appLabel)
		if err != nil {
			fmt.Printf("error %v\n", err)
			http.Error(w, "failed to retrieve pods", http.StatusInternalServerError)
			return
		}

		logsChannel := make(chan string)
		fetchPodLogs(client, logsChannel, namespace, podNames, int64(tailLines), watch)

		writerFlusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "not flushable", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)

		defer close(logsChannel)

		for text := range logsChannel {
			if _, err := w.Write([]byte(fmt.Sprintf("%s\n", text))); err != nil {
				fmt.Printf("error %v\n", err)
				http.Error(w, "could not write chunk", http.StatusInternalServerError)
				return
			}
			writerFlusher.Flush()
		}
	}
}

func main() {
	r := mux.NewRouter()
	r.Use(mux.CORSMethodMiddleware(r))
	r.Path("/v1/logs").HandlerFunc(logsHandler()).Methods(http.MethodGet)

	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:9000",
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	fmt.Println("[INFO] Server listening on 0.0.0.0:9000")
	log.Fatal(srv.ListenAndServe())

	// curl -N -s "http://localhost:9000/v1/logs?selector_key=k8s-app&selector_value=kube-dns&namespace=kube-system&tail_lines=100"
	// curl -N -s "http://localhost:9000/v1/logs?selector_key=component&selector_value=etcd&namespace=kube-system&tail_lines=100"
}
