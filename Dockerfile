FROM golang:1.11.0

ENV GOPATH=/go

WORKDIR /go/src/github.com/EP4/kubernetes-ssh-container-exposer

RUN mkdir -p "${GOPATH}/src/github.com/golang" \
 && cd "${GOPATH}/src/github.com/golang" \
 && mkdir -p "${GOPATH}/bin" \
 && curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

ADD . "${WORKDIR}"

RUN dep ensure

RUN go build -o kubernetes-ssh-container-exposer cmd.go

CMD "./kubernetes-ssh-container-exposer"