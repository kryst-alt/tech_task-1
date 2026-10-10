# Build from the repository root: docker build -f services/sheets-sync/Dockerfile .
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY db ./db
COPY internal ./internal
COPY services/sheets-sync ./services/sheets-sync
RUN CGO_ENABLED=0 go build -o /out/sheets-sync ./services/sheets-sync

FROM docker.io/library/alpine:3.22
RUN adduser -D app
USER app
COPY --from=build /out/sheets-sync /sheets-sync
ENTRYPOINT ["/sheets-sync"]
