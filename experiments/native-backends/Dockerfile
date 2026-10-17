ARG RUST_IMAGE=rust@sha256:e51d0265072d2d9d5d320f6a44dde6b9ef13653b035098febd68cce8fa7c0bc4
FROM ${RUST_IMAGE}
ENV CI=1 DEBIAN_FRONTEND=noninteractive CARGO_HOME=/probe/cargo
RUN apt-get update </dev/null && apt-get install -y --no-install-recommends clang-14=1:14.0.6-12 llvm-14=1:14.0.6-12 </dev/null && rm -rf /var/lib/apt/lists/*
WORKDIR /probe/src
COPY cranelift/ .
RUN cargo build --locked --release </dev/null
RUN dpkg-query -W > /probe/packages.txt
