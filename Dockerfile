FROM alpine:3.6

RUN apk --no-cache add ca-certificates tini curl bash

COPY /jiralert /jiralert
CMD chmod 755 /jiralert

ENTRYPOINT ["/bin/bash", "-c", "/jiralert \"$@\"", "--"]