FROM golang:1.10.3

WORKDIR /go/src/github.com/ep4/kubernetes-ssh-container-exposer

ADD . .

RUN go install github.com/ep4/kubernetes-ssh-container-exposer

CMD "kubernetes-ssh-container-exposer"