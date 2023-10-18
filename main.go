package main

import (
	"bufio"
	"context"
	"encoding/json"
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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
  client *kubernetes.Clientset
  runningInKubernetes bool
  defaultSelectorKey string
  defaultNamespace string
  defaultTailLines string
  xApiKey string
)

func init() {
	// parameter for kubernetes client auth
	runningInKubernetes, _ = strconv.ParseBool(getValueOrDefault(os.Getenv("RUNNING_IN_KUBERNETES"), "0").(string))
	// kubernetes client
	var config *rest.Config
	var err error
	if runningInKubernetes {
		// InClusterConfig uses the service account bound to this service
		config, err = rest.InClusterConfig()
	} else {
		kubeconfigPath := getValueOrDefault(os.Getenv("KUBECONFIG"), os.Getenv("HOME")+"/.kube/config").(string)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if err != nil {
		fmt.Printf("Error creating kubernetes client: %v", err)
		panic(err)
	}
	client, _ = kubernetes.NewForConfig(config)
	// parameters for logs
	defaultSelectorKey = getValueOrDefault(os.Getenv("DEFAULT_SELECTOR_KEY"), "k8s-app").(string)
	defaultNamespace = getValueOrDefault(os.Getenv("DEFAULT_NAMESPACE"), "default").(string)
	defaultTailLines = getValueOrDefault(os.Getenv("DEFAULT_TAIL_LINES"), "1000").(string)
	// api key
	xApiKey = os.Getenv("X_API_KEY")
	if xApiKey == "" {
		log.Fatal("Missing X Api Key")
	}
}

type HttpResponseMessage struct {
	Message interface{} `json:"message"`
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

func authenticationMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := strings.Replace(r.Header.Get("Authorization"), "X-Api-Key ", "", 1)
			if tokenString == "" {
				fmt.Println("Unauthorized - Invalid Authorization Header")
				http.Error(w, "Unauthorized - Invalid Authorization Header", http.StatusUnauthorized)
				return
			}
			if tokenString != xApiKey {
				fmt.Println("Unauthorized - Invalid Token")
				http.Error(w, "Unauthorized - Invalid Token", http.StatusUnauthorized)
				return
			}
			// call next handler
			next.ServeHTTP(w, r)
		})
	}
}

func fetchContainerNamesFromLabel(client *kubernetes.Clientset, namespace string,
	label string) ([]string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	checkContainers := true
	for _, pod := range pods.Items {
		// only retrieve container names in the first iteration,
		// as the other pods for this app will have the same containers
		if checkContainers {
			for _, container := range pod.Spec.Containers {
				containerName := container.Name
				// not fetching logs of sidecar/proxy containers
				if sliceContains(containerNames, containerName) ||
					stringContains(containerName, "sidecar", "proxy") {
					continue
				}
				containerNames = append(containerNames, containerName)
			}
			checkContainers = false
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

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		fmt.Printf("Healthcheck")
		response, _ := json.Marshal(&HttpResponseMessage{Message: "Healthy"})
		w.Write([]byte(string(response)))
	}
}

func logsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=UTF-8")
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
	// add routes
	subRouterHealth := r.PathPrefix("/v1/health").Subrouter()
	subRouterHealth.Path("").HandlerFunc(healthHandler()).Methods(http.MethodGet)
	subRouterLogs := r.PathPrefix("/v1/logs").Subrouter()
	subRouterLogs.Use(authenticationMiddleware())
	subRouterLogs.Path("").HandlerFunc(logsHandler()).Methods(http.MethodGet)
	// create server
	srv := &http.Server{
		Handler:     r,
		Addr:        "0.0.0.0:9000",
		IdleTimeout: 30 * time.Second,
		// it will not have other timeouts due streaming of logs
	}
	fmt.Println("[INFO] Server listening on 0.0.0.0:9000")
	log.Fatal(srv.ListenAndServe())
}
