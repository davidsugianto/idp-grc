IMG ?= controller:latest
ENVTEST_K8S_VERSION = 1.30.0
LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: all build test generate manifests vet fmt docker-build docker-push deploy undeploy install uninstall

all: build

build:
	go build -o bin/manager ./cmd/...

run:
	go run ./cmd/main.go

test: generate manifests envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

vet:
	go vet ./...

fmt:
	go fmt ./...

generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests: controller-gen
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

docker-build:
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

install: manifests
	kubectl apply -f config/crd/bases/

uninstall:
	kubectl delete -f config/crd/bases/

deploy: manifests
	cd config/default && kubectl apply -k .

undeploy:
	cd config/default && kubectl delete -k .

controller-gen:
	@mkdir -p $(LOCALBIN)
	@test -s $(CONTROLLER_GEN) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.15.0

envtest:
	@mkdir -p $(LOCALBIN)
	@test -s $(ENVTEST) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
