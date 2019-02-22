FROM golang:1.8

COPY /jiralert /jiralert
CMD chmod 755 /jiralert

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]