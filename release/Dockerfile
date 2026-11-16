FROM alpine:3.22
RUN apk --no-cache add ca-certificates
ARG TARGETARCH
COPY bin/linux-${TARGETARCH}/cloudharbor /app/cloudharbor
RUN chmod +x /app/cloudharbor
WORKDIR /app
EXPOSE 8080
ENV SERVER_HOST=0.0.0.0 SERVER_PORT=8080
ENTRYPOINT ["/app/cloudharbor"]
