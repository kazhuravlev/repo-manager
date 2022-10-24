FROM alpine:3.16

ENV WORKDIR=/workdir

RUN mkdir -p ${WORKDIR}

WORKDIR ${WORKDIR}
VOLUME ${WORKDIR}

ENTRYPOINT ["/bin/repo-manager"]

COPY repo-manager /bin/repo-manager
