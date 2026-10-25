FROM node:22-bookworm-slim
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
ARG BACKEND_URL=http://api:8000
ARG NEXT_PUBLIC_SUPABASE_URL=https://vocvwmiypifqnncxhfpj.supabase.co
ARG NEXT_PUBLIC_SUPABASE_ANON_KEY
ENV BACKEND_URL=$BACKEND_URL
ENV NEXT_PUBLIC_SUPABASE_URL=$NEXT_PUBLIC_SUPABASE_URL
ENV NEXT_PUBLIC_SUPABASE_ANON_KEY=$NEXT_PUBLIC_SUPABASE_ANON_KEY
RUN npm run build
EXPOSE 3000
CMD ["npm", "start", "--", "-H", "0.0.0.0", "-p", "3000"]
