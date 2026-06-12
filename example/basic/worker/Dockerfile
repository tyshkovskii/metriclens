FROM golang:1.26.4-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app .

FROM scratch
COPY --from=build /out/app /app
EXPOSE 9090
ENTRYPOINT ["/app"]
