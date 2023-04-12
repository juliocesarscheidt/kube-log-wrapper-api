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
var defaultSelectorKey string
var defaultNamespace string
var defaultTailLines string

func init() {
	// kubernetes client
	kubeconfig := getValueOrDefault(os.Getenv("KUBECONFIG"), "/root/.kube/config").(string)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Printf("error %v", err)
		panic(err)
	}
	client, _ = kubernetes.NewForConfig(config)
	// parameters for logs
	defaultSelectorKey = getValueOrDefault(os.Getenv("DEFAULT_SELECTOR_KEY"), "k8s-app").(string)
	defaultNamespace = getValueOrDefault(os.Getenv("DEFAULT_NAMESPACE"), "default").(string)
	defaultTailLines = getValueOrDefault(os.Getenv("DEFAULT_TAIL_LINES"), "1000").(string)
}

func getValueOrDefault(value interface{}, defaultValue interface{}) interface{} {
	if value.(string) == "" {
		return defaultValue
	}
	return value
}

func stringContains(sourceString string, targetStrings ...string) bool {
	for _, str := range targetStrings {
		if strings.Contains(sourceString, str) {
			return true
		}
	}
	return false
}

func sliceContains(sourceSlice []string, targetStrings ...string) bool {
	for _, str := range targetStrings {
		if slices.Contains(sourceSlice, str) {
			return true
		}
	}
	return false
}

func fetchContainerNamesFromLabel(client *kubernetes.Clientset, namespace string,
	label string) ([]string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// retrieve pods using label selector
	opts := metav1.ListOptions{LabelSelector: label}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		fmt.Printf("Error retrieving pods: %v\n", err)
		return nil, nil, err
	}
	podNames := []string{}
	containerNames := []string{}
	iterationPodsCounter := 0
	for _, pod := range pods.Items {
		// only retrieve container names in the first iteration,
		// as the other pods for this app will have the same containers
		if iterationPodsCounter == 0 {
			for _, container := range pod.Spec.Containers {
				containerName := container.Name
				if sliceContains(containerNames, containerName) ||
					stringContains(containerName, "sidecar", "proxy") {
					continue
				}
				containerNames = append(containerNames, containerName)
			}
			iterationPodsCounter = iterationPodsCounter + 1
		}
		podNames = append(podNames, pod.Name)
	}
	return podNames, containerNames, nil
}

func fetchPodsContainerLogs(client *kubernetes.Clientset, logsChannel chan string,
	namespace string, podNames []string, containerName string, tailLines int64) {
	ctx := context.Background()
	opts := &corev1.PodLogOptions{
		Container:    containerName,
		Follow:       true,
		SinceSeconds: nil,
		Timestamps:   true,
		TailLines:    &tailLines,
	}
	for _, podName := range podNames {
		// retrieve logs inside goroutines and send to channel
		go func(podName, containerName string, logsChannel chan string) {
			fmt.Printf("podName %s\n", podName)
			fmt.Printf("containerName %s\n", containerName)
			// request to retrieve logs in streaming fashion
			req := client.CoreV1().Pods(namespace).GetLogs(podName, opts)
			logs, err := req.Stream(ctx)
			if err != nil {
				fmt.Printf("Error retrieving logs: %v\n", err)
				panic(err)
			}
			sc := bufio.NewScanner(logs)
			for sc.Scan() {
				logsChannel <- sc.Text()
			}
		}(podName, containerName, logsChannel)
	}
}

func processRetrieveLogsToChannel(client *kubernetes.Clientset, logsChannel chan string, namespace string,
	selectorKey string, selectorValue string, tailLines int64) error {
	label := fmt.Sprintf("%s=%s", selectorKey, selectorValue)
	fmt.Printf("Using label %s\n", label)
	podNames, containerNames, err := fetchContainerNamesFromLabel(client, namespace, label)
	if err != nil {
		return err
	}
	for _, containerName := range containerNames {
		fetchPodsContainerLogs(client, logsChannel, namespace, podNames, containerName, tailLines)
	}
	return nil
}

func logsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// retrieve parameters
		selectorKey := getValueOrDefault(r.FormValue("selectorKey"), defaultSelectorKey).(string)
		selectorValue := r.FormValue("selectorValue")
		if selectorValue == "" {
			http.Error(w, "Missing selector value", http.StatusBadRequest)
			return
		}
		namespace := getValueOrDefault(r.FormValue("namespace"), defaultNamespace).(string)
		tailLines, _ := strconv.Atoi(getValueOrDefault(r.FormValue("tailLines"), defaultTailLines).(string))
		// create writter flusher
		writerFlusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Not flushable", http.StatusInternalServerError)
			return
		}
		// open channel to receive logs from multiple pods
		logsChannel := make(chan string)
		err := processRetrieveLogsToChannel(client, logsChannel, namespace,
			selectorKey, selectorValue, int64(tailLines))
		if err != nil {
			fmt.Printf("Error retrieving logs: %v\n", err)
			http.Error(w, "Failed to retrieve pods", http.StatusInternalServerError)
			return
		}
		// start streaming logs
		w.WriteHeader(http.StatusOK)
		for text := range logsChannel {
			if _, err := w.Write([]byte(fmt.Sprintf("%s\n", text))); err != nil {
				fmt.Printf("Error writing data: %v\n", err)
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
		// it will not have without due to streaming
		// ReadTimeout: 5 * time.Second,
		// WriteTimeout: 5 * time.Second,
		// ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Println("[INFO] Server listening on 0.0.0.0:9000")
	log.Fatal(srv.ListenAndServe())
}
