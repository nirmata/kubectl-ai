.PHONY: manifest
manifest:
	helm repo add bitnami https://charts.bitnami.com/bitnami
	helm template \
  		mysql oci://registry-1.docker.io/bitnamicharts/mysql --version 9.10.4 \
  		--namespace list-images-for-pods \
  		--create-namespace > manifest.yaml
