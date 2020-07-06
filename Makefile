all:
    DOCKER_USERNAME="alexheld"
    REPO="site"
    IMAGE="$DOCKER_USERNAME/$REPO"

.#PHONY: docker-build
docker-build:
    docker build -t alexheld/site:$(echo $GITHUB_SHA | head -c7) .

.#PHONY: docker-publish
docker-publish:
    echo $DOCKER_PASSWORD | docker login -u $DOCKER_USERNAME --password-stdin
    docker push $IMAGE
