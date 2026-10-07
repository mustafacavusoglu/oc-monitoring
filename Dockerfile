FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# Precompress hashed assets so the server sends them gzipped with zero CPU cost.
RUN npm run build && find dist/assets -type f \( -name '*.js' -o -name '*.css' \) -exec sh -c 'gzip -9c "$1" > "$1.gz"' _ {} \;

# Free-threaded (no-GIL) CPython, the stripped python-build-standalone build.
FROM debian:bookworm-slim AS backend
ARG TARGETARCH
ARG PYTHON=3.14.8+20261003
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && arch=$([ "$TARGETARCH" = arm64 ] && echo aarch64 || echo x86_64) \
    && curl -fsSL "https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON#*+}/cpython-${PYTHON}-${arch}-unknown-linux-gnu-freethreaded-install_only_stripped.tar.gz" \
       | tar -xz -C /opt
COPY requirements.txt /app/
RUN /opt/python/bin/python3.14t -m pip install --no-cache-dir --target /app/deps -r /app/requirements.txt \
    && cd /opt/python && rm -rf include share lib/*.a lib/libtcl* lib/libtk* lib/itcl* lib/tcl* lib/tk* lib/thread* \
       lib/python3.14t/config-* lib/python3.14t/test lib/python3.14t/idlelib lib/python3.14t/tkinter \
       lib/python3.14t/ensurepip lib/python3.14t/site-packages/pip* lib/python3.14t/lib-dynload/_tkinter*
COPY monitor/ /app/monitor/
RUN /opt/python/bin/python3.14t -m compileall -q /app

FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=backend /opt/python /opt/python
COPY --from=backend /app /app
COPY --from=frontend /web/dist /app/web
ENV PYTHONPATH=/app/deps:/app \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHON_GIL=0 \
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
EXPOSE 8080
ENTRYPOINT ["/opt/python/bin/python3.14t", "-m", "monitor.server"]
