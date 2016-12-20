version := 0.0.1
tagBase := foolusion/rt53-updater
tag := ${tagBase}:${version}
all: kube.yaml

rt53-updater: *.go
	GOOS=linux go build -a --ldflags '-extldflags "-static"' -tags netgo -installsuffix netgo .

docker/build: rt53-updater
	docker build -t ${tagBase} \
		--build-arg HTTP_PROXY=http://webproxysea.nordstrom.net:8181 \
		--build-arg http_proxy=http://webproxysea.nordstrom.net:8181 \
		--build-arg HTTPS_PROXY=http://webproxysea.nordstrom.net:8181 \
		--build-arg https_proxy=http://webproxysea.nordstrom.net:8181 \
		--build-arg NO_PROXY="127.0.0.1,169.254.169.254,nordstrom.net,dev.nordstrom.com,website.nordstrom.com,wsperf.nordstrom.com,192.168.99.100" \
		--build-arg no_proxy="127.0.0.1,169.254.169.254,nordstrom.net,dev.nordstrom.com,website.nordstrom.com,wsperf.nordstrom.com,192.168.99.100" \
		.
docker/tag: docker/build
	docker tag ${tagBase} ${tag}

docker/push: docker/tag
	docker push ${tag}

kube.yaml: docker/push
	sed 's#{{.Image}}#${tag}#;' rt53-updater.yaml > kube.yaml

deploy: clean kube.yaml
	kubectl apply --record -f kube.yaml

clean:
	$(RM) elwin

	
