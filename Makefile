PROJECT_PATH := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

.PHONY: testing.unit
testing.unit: ## Runs unit tests
	go test ./...

.PHONY: testing.all
testing.all: testing.mysql # Runs unit and integration tests
	sleep 5
	docker exec mysql bash -c  'until [[ `mysql -u "root" -e "show databases;"| grep sshpiper | wc -l` == "1" ]]; do sleep 5; done'
	go test ./... -tags=integration

.PHONY: testing.mysql
testing.mysql: ## Runs mysql preconfigured for integration testing
	docker-compose -f $(PROJECT_PATH)/scripts/testing/mysql/docker-compose.yml up -d --force-recreate

.PHONY: testing.mysql-clean
testing.mysql-clean: ## Cleans mysql environment
	docker-compose -f $(PROJECT_PATH)/scripts/testing/mysql/docker-compose.yml down