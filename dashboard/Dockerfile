FROM node:20-alpine AS builder

WORKDIR /app

COPY dashboard/package.json ./
RUN npm install

COPY dashboard/ ./

# Set API proxy to gateway for dev, but for production build we use relative paths
# The dashboard will be served behind nginx which proxies /v1/* to gateway
RUN npm run build

FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html

# nginx config to proxy API calls to gateway
RUN echo 'server { \
  listen 80; \
  location / { \
    root /usr/share/nginx/html; \
    try_files $uri $uri/ /index.html; \
  } \
  location /v1/ { \
    proxy_pass http://gateway:8080; \
    proxy_set_header Host $host; \
  } \
  location /v1/containment/ { \
    proxy_pass http://containment-controller:8083; \
    proxy_set_header Host $host; \
  } \
  location /v1/budget-nodes { \
    proxy_pass http://budget-ledger:8082; \
    proxy_set_header Host $host; \
  } \
  location /v1/budget-node-by-label/ { \
    proxy_pass http://budget-ledger:8082; \
    proxy_set_header Host $host; \
  } \
  location /v1/agents { \
    proxy_pass http://identity-service:8081; \
    proxy_set_header Host $host; \
  } \
  location /v1/approvals { \
    proxy_pass http://containment-controller:8083; \
    proxy_set_header Host $host; \
  } \
  location /v1/emergency-stop { \
    proxy_pass http://containment-controller:8083; \
    proxy_set_header Host $host; \
  } \
}' > /etc/nginx/conf.d/default.conf

EXPOSE 80
