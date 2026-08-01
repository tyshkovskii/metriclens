FROM python:3.13-slim

WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --requirement requirements.txt

COPY main.py ./
EXPOSE 8000
ENTRYPOINT ["python", "main.py"]
