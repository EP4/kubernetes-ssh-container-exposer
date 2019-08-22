package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/EP4/kubernetes-ssh-container-exposer/internal/registry"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

const testNamespace = "test"
const validNames = "test-ssh"
const staticClusterIP = "127.0.0.1"

var resultChan = make(chan interface{})

func TestSSHSecretHandlerCreate(t *testing.T) {
	c := fake.NewSimpleClientset()
	l, _ := zap.NewDevelopment()
	handler := NewSecretHandler(c, mockRegistry{}, l)

	ch := handler.NewCreateHandler()

	secret, s, b64 := getValidSSHSecret(t)
	secret, err := c.CoreV1().Secrets(testNamespace).Create(secret)
	if err != nil {
		t.Errorf("error when creating test secret")
	}

	service := getValidSSHService(t)
	_, err = c.CoreV1().Services(testNamespace).Create(service)
	if err != nil {
		t.Errorf("error when creating test service")
	}

	// mimic the behaviour guaranteed by our controller when a create method is called
	ch.SetObject(secret)

	err = ch.Handle()
	if err != nil {
		t.Errorf("unexpected error when handling create event - %v", err)
	}

	upstream := <-resultChan
	result, ok := upstream.(*registry.Upstream)
	if !ok {
		t.Errorf("unexpected type assertion - got %v", result)
	}

	expect := &registry.Upstream{
		Name:                validNames,
		Username:            validNames,
		Address:             staticClusterIP,
		SSHPiperPrivateKey:  s,
		DownstreamPublicKey: b64,
	}

	if !reflect.DeepEqual(result, expect) {
		t.Errorf("unexpected result, expected \n %v \n but got \n%v", expect, result)
	}
}

// getValidSSHSecret expected to be parsed during happy path test
func getValidSSHSecret(t *testing.T) (*v1.Secret, string, []string) {
	t.Helper()

	pk, key := generatePublicKey(t)

	secret := &v1.Secret{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "secret",
			APIVersion: "v1",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name: validNames,
		},
		Data: map[string][]byte{
			"sshpiper_id_rsa":       key,
			"downstream_id_rsa.pub": key,
		},
	}

	return secret, string(key), []string{base64.StdEncoding.EncodeToString(pk.Marshal())}
}

func generatePublicKey(t *testing.T) (ssh.PublicKey, []byte) {
	t.Helper()
	bitSize := 4096

	privateKey, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		t.Errorf("failed to generate required key")
	}

	if err = privateKey.Validate(); err != nil {
		t.Errorf("error validating private key")
	}

	pubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Errorf("failed to generate required public key")
	}

	parseable := ssh.MarshalAuthorizedKey(pubKey)
	pk, _, _, _, err := ssh.ParseAuthorizedKey(parseable)
	if err != nil {
		t.Errorf("failed to parse authorized key")
	}
	return pk, parseable
}

func getValidSSHService(t *testing.T) *v1.Service {
	t.Helper()
	return &v1.Service{
		TypeMeta: metaV1.TypeMeta{
			Kind: "service",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name: validNames,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:     "ssh",
					Protocol: "TCP",
					Port:     SSHServicePort,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: SSHServicePort,
					},
				},
			},
			ClusterIP: staticClusterIP,
		},
	}
}

type mockRegistry struct{}

func (mr mockRegistry) RegisterUpstream(upstream *registry.Upstream) (*registry.Upstream, error) {
	go func() { resultChan <- upstream }()
	return upstream, nil
}
