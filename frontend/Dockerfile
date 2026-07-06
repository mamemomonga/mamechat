FROM node:24-alpine

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
# version.full(make版) があれば優先、無ければ version。vite.config が ../version* を読む。
COPY version* /app/
COPY frontend/ ./
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
