# Build from the repository root, because the AI Service contract lives in
# proto/ at the root (ADR-005, ADR-007):
#
#   docker build -f services/resume-processing/Dockerfile -t hireassist/resume-processing:2.0.0 .

FROM node:22-alpine

WORKDIR /app

# Production dependencies first, so this layer is cached
COPY services/resume-processing/package.json services/resume-processing/package-lock.json ./
RUN npm ci --omit=dev

COPY services/resume-processing/server.js ./
COPY services/resume-processing/src ./src
COPY proto /proto

ENV PORT=3000 \
    AI_PROTO_PATH=/proto/hireassist/ai/v1/ai_service.proto
EXPOSE 3000

USER node
CMD ["node", "server.js"]
