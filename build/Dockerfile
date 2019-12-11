FROM registry.redhat.io/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/xzbc-redis-cluster \
    USER_UID=1001 \
    USER_NAME=xzbc-redis-cluster

# install operator binary
COPY build/_output/bin/xzbc-redis-cluster ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  chmod +x /usr/local/bin/user_setup && /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
