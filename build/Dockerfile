# Build the community continuous profiling
ARG DOCKER_BUILD_IMAGE=golang:1.14.10
ARG DOCKER_BASE_IMAGE=alpine:3.12

FROM ${DOCKER_BUILD_IMAGE} AS build
WORKDIR /community-continuous-profiling/
COPY . /community-continuous-profiling/
RUN apt-get update -yq && apt-get install -yq unzip
RUN make build


# Final Image
FROM ${DOCKER_BASE_IMAGE}
LABEL name="Mattermost Community Continuous Profiling" \
  maintainer="cloud-team@mattermost.com" \
  vendor="Mattermost" \
  distribution-scope="private" \
  architecture="x86_64" \
  url="https://mattermost.com" \
  io.k8s.description="Mattermost Community Continuous Profiling is responsible for running cpu and memory profiles for community and community daily" \
  io.k8s.display-name="Mattermost Community Continuous Profiling"

ENV CLOUD=/community-continuous-profiling/main \
    USER_UID=10001 \
    USER_NAME=cloud

RUN  apk update && apk add libc6-compat && apk add ca-certificates && apk --no-cache add curl
COPY --from=build /community-continuous-profiling/build/_output/bin/main /community-continuous-profiling/main
COPY --from=build /community-continuous-profiling/build/bin /usr/local/bin

RUN touch /community-continuous-profiling/community_mem.prof
RUN touch /community-continuous-profiling/community_cpu.prof
RUN touch /community-continuous-profiling/community-daily_mem.prof
RUN touch /community-continuous-profiling/community-daily_cpu.prof

RUN  /usr/local/bin/user_setup
WORKDIR /community-continuous-profiling/

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
