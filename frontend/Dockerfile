FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM nginx:1.29-alpine
COPY --from=builder /app/dist /usr/share/nginx/html/app
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
EXPOSE 80
ENV BACKEND_HOST=backend
