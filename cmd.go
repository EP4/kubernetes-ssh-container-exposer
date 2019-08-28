package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EP4/kubernetes-ssh-container-exposer/internal/handlers"
	internalLogger "github.com/EP4/kubernetes-ssh-container-exposer/internal/logger"
	"github.com/EP4/kubernetes-ssh-container-exposer/internal/registry"
	controller "github.com/philipgough/kube-kontroller"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var logger, _ = zap.NewDevelopment()

const VERSION = "0.2.0"

func newClient(outOfCluster bool) (kubernetes.Interface, error) {
	if !outOfCluster {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		return kubernetes.NewForConfig(config)
	}

	homeDir := func() string {
		if h := os.Getenv("HOME"); h != "" {
			return h
		}
		return os.Getenv("USERPROFILE") // windows
	}

	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	return kubernetes.NewForConfig(config)
}

func initializeRegistry() (*registry.Registry, error) {
	registry := registry.NewRegistry(logger)
	if err := registry.ConnectDatabase(); err != nil {
		return nil, err
	}
	if err := registry.TruncateAll(); err != nil {
		return nil, err
	}
	return registry, nil
}

func main() {
	logger.Info("Started", zap.String("version", VERSION))
	logger.WithOptions()

	registry, err := initializeRegistry()
	if err != nil {
		logger.Fatal(fmt.Sprintf("failed to initialize registry - %v", err.Error()))
	}

	kubeClient, err := newClient(false)
	if err != nil {
		logger.Fatal(fmt.Sprintf("failed to create Kubernetes client - %v", err.Error()))
	}

	ctrlLogger := internalLogger.NewLogger(logger)

	ctrl := controller.NewSecretController(kubeClient, controller.GetDefaultOptions(), controller.GetDefaultListOpts(), ctrlLogger)
	ctrl.SetHandlerFactory(handlers.NewSecretHandler(kubeClient, registry, logger))

	stopCh := make(chan struct{})
	go ctrl.Run(stopCh)

	<-stopCh
}
