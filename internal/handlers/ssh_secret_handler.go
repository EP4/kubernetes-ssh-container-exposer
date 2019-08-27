package handlers

import (
	"bytes"
	"database/sql"
	"encoding/base64"

	"github.com/EP4/kubernetes-ssh-container-exposer/internal/registry"
	controller "github.com/EP4/kubernetes-ssh-container-exposer/pkg/kubernetes/secrets-controller"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const SSHServicePort int32 = 22

type (
	Services []v1.Service
	Keys     struct {
		SSHPiperPrivateKey  string
		DownstreamPublicKey []string
	}
)

type (
	SSHSecretHandler struct {
		client   kubernetes.Interface
		registry registry.Registrable
		logger   *zap.Logger
	}

	CreateResourceHandler struct {
		client   kubernetes.Interface
		registry registry.Registrable
		logger   *zap.Logger
		newValue interface{}
	}

	UpdateResourceHandler struct {
		client   kubernetes.Interface
		registry registry.Registrable
		logger   *zap.Logger
		newValue interface{}
		oldValue interface{}
	}

	DeleteResourceHandler struct {
		oldValue interface{}
	}
)

func NewSecretHandler(c kubernetes.Interface, r registry.Registrable, l *zap.Logger) SSHSecretHandler {
	return SSHSecretHandler{
		client:   c,
		registry: r,
		logger:   l,
	}
}

func (h SSHSecretHandler) NewCreateHandler() controller.HandleCreate {
	return &CreateResourceHandler{
		client:   h.client,
		registry: h.registry,
		logger:   h.logger,
	}
}

func (h SSHSecretHandler) NewUpdateHandler() controller.HandleUpdate {
	return &UpdateResourceHandler{
		client:   h.client,
		registry: h.registry,
		logger:   h.logger,
	}
}

func (h SSHSecretHandler) NewDeleteHandler() controller.HandleDelete {
	return &DeleteResourceHandler{}
}

func (ch *CreateResourceHandler) Handle() error {
	secret, ok := ch.newValue.(*v1.Secret)
	if !ok {
		// failed type assertion here - something has gone wrong and retrying wont help - remove it from the queue
		// TODO - add logging
		return nil
	}

	service, err := getSSHService(secret.Name, secret.Namespace, ch.client)
	if err != nil || service == nil {
		// this is likely not worth retrying but might want to add some relevant logging
		return nil
	}

	keys, err := parseSecretKeys(secret)
	if err != nil {
		// its debatable if we should actually retry this work here again
		// might be worth invalidating specific secrets and services
		// TODO - add logging
		return nil
	}

	upstream := &registry.Upstream{
		Name:                secret.Name,
		Username:            secret.Name,
		Address:             service.Spec.ClusterIP,
		SSHPiperPrivateKey:  keys.SSHPiperPrivateKey,
		DownstreamPublicKey: keys.DownstreamPublicKey,
	}

	return registerUpstream(ch.registry, upstream)
}

func (ch *CreateResourceHandler) SetObject(object interface{}) {
	ch.newValue = object
}

func (uh *UpdateResourceHandler) Handle() error {
	old, ok := uh.oldValue.(*v1.Secret)
	if !ok {
		return nil
	}

	new, ok := uh.newValue.(*v1.Secret)
	if !ok {
		return nil
	}

	if old.ResourceVersion != new.ResourceVersion {
		service, err := getSSHService(new.Name, new.Namespace, uh.client)
		if err != nil || service == nil {
			// this is likely not worth retrying but might want to add some relevant logging
			return nil
		}

		u, err := getUpstreamFromSecret(new)
		if err != nil {
			// its debatable if we should actually retry this work here again
			// might be worth invalidating specific secrets and services
			// TODO - add logging
			return nil
		}

		u.Address = service.Spec.ClusterIP
		return registerUpstream(uh.registry, u)
	} else {
		// nothing to do
		return nil
	}
}

func (uh *UpdateResourceHandler) SetObjects(old, new interface{}) {
	uh.oldValue = old
	uh.newValue = new
}

func (dh *DeleteResourceHandler) Handle() error {
	return nil
}

func (dh *DeleteResourceHandler) SetObject(object interface{}) {
	dh.oldValue = object
}

//// utility functions to be used by handlers ///////

func hasPort(servicePorts []v1.ServicePort, port int32) bool {
	for _, servicePort := range servicePorts {
		if servicePort.Port == port {
			return true
		}
	}
	return false
}

func parseSecretKeys(secret *v1.Secret) (Keys, error) {
	var downstreamPublicKeys []string
	secretDownstreamPublicKeys := bytes.Split(secret.Data["downstream_id_rsa.pub"], []byte("\n"))

	for _, downstreamPublicKey := range secretDownstreamPublicKeys {
		if string(downstreamPublicKey) == "" {
			continue
		}
		byteDownstreamPublicKey, _, _, _, err := ssh.ParseAuthorizedKey(downstreamPublicKey)
		if err != nil {
			return Keys{}, err
		} else {
			downstreamPublicKeys = append(downstreamPublicKeys, base64.StdEncoding.EncodeToString(byteDownstreamPublicKey.Marshal()))
		}

	}
	return Keys{
		SSHPiperPrivateKey:  string(secret.Data["sshpiper_id_rsa"]),
		DownstreamPublicKey: downstreamPublicKeys,
	}, nil
}

func registerUpstream(r registry.Registrable, upstream *registry.Upstream) error {
	_, err := r.RegisterUpstream(upstream)
	return err
}

// getSSHService corresponding to the name, typically provided from the secret
func getSSHService(name, namespace string, client kubernetes.Interface) (*v1.Service, error) {
	service, err := client.CoreV1().Services(namespace).Get(name, metaV1.GetOptions{})
	if err != nil || service == nil {
		return nil, err
	}

	if !hasPort(service.Spec.Ports, SSHServicePort) {
		return nil, nil
	}
	return service, nil
}

func getUpstreamFromSecret(s *v1.Secret) (*registry.Upstream, error) {
	keys, err := parseSecretKeys(s)
	if err != nil {
		return nil, err
	}

	upstream := &registry.Upstream{
		Name:                s.Name,
		Username:            s.Name,
		SSHPiperPrivateKey:  keys.SSHPiperPrivateKey,
		DownstreamPublicKey: keys.DownstreamPublicKey,
	}
	return upstream, nil
}