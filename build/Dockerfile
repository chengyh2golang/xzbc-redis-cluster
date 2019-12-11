FROM registry.redhat.io/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/xzbc-temp \
    USER_UID=1001 \
    USER_NAME=xzbc-temp

# install operator binary
COPY build/_output/bin/xzbc-temp ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  chmod +x /usr/local/bin/user_setup && /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
