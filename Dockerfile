FROM golang as build
COPY . /site
WORKDIR /site
RUN CGO_ENABLED=0 GOBIN=/root go install -v ./cmd/site


FROM golang
EXPOSE 443
EXPOSE 5000
WORKDIR /site
COPY --from=build /root/site .
COPY --from=build /site/content .
HEALTHCHECK CMD wget --spider http://127.0.0.1:5000/.within/health || exit 1
CMD ./site

