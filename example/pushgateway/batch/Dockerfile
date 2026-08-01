FROM alpine:3.21

RUN apk add --no-cache curl
COPY push.sh /usr/local/bin/push.sh
RUN chmod +x /usr/local/bin/push.sh

ENTRYPOINT ["/usr/local/bin/push.sh"]
