FROM node:20-alpine

RUN apk add --no-cache git
WORKDIR /app

COPY package.json ./
RUN npm install

COPY hardhat.config.js ./
COPY contracts ./contracts
COPY scripts ./scripts

CMD ["npm", "run", "node"]
