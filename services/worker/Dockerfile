FROM python:3.12-slim AS builder
WORKDIR /build
COPY pyproject.toml .
ARG PIP_TRUSTED_HOST=
RUN pip install --no-cache-dir --upgrade pip ${PIP_TRUSTED_HOST:+--trusted-host pypi.org --trusted-host files.pythonhosted.org} \
 && pip install --no-cache-dir ${PIP_TRUSTED_HOST:+--trusted-host pypi.org --trusted-host files.pythonhosted.org} "."

FROM python:3.12-slim
WORKDIR /app

# Create non-root user
RUN groupadd -r worker && useradd -r -g worker worker

# Copy installed packages from builder
COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

# Copy application source
COPY worker/ ./worker/

RUN chown -R worker:worker /app
USER worker

EXPOSE 8081

CMD ["python", "-m", "worker.main"]
