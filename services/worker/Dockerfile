# syntax=docker/dockerfile:1

# ---- builder: resolve & install dependencies into a venv using uv ----
FROM python:3.12-slim AS builder

# Pinned uv binary, copied from the official image.
COPY --from=ghcr.io/astral-sh/uv:0.11 /uv /bin/uv

WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=0

# Behind a TLS-intercepting proxy (e.g. a corporate network), build with:
#   docker build --build-arg UV_INSECURE_HOST="pypi.org files.pythonhosted.org" .
# Off by default (secure); mirrors the old PIP_TRUSTED_HOST opt-in.
ARG UV_INSECURE_HOST=
ENV UV_INSECURE_HOST=${UV_INSECURE_HOST}

# Install ONLY dependencies first — cached unless pyproject.toml / uv.lock change.
# --no-dev excludes the test group; project itself isn't installed (package = false).
COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev

# ---- runtime: slim, non-root, just the venv + source ----
FROM python:3.12-slim
WORKDIR /app

RUN groupadd -r worker && useradd -r -g worker worker

# The resolved virtual environment from the builder.
COPY --from=builder /app/.venv /app/.venv
# Application source (run from source; see `package = false` in pyproject.toml).
COPY worker/ ./worker/

# Use the venv's interpreter and entry points.
ENV PATH="/app/.venv/bin:$PATH"

RUN chown -R worker:worker /app
USER worker

EXPOSE 8081
CMD ["python", "-m", "worker.main"]
