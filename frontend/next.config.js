/** @type {import('next').NextConfig} */
const backendURL = process.env.NEXT_PUBLIC_BACKEND_URL || 'http://localhost:8080';

const nextConfig = {
  output: 'standalone',
  async rewrites() {
    return [
      // Proxy WebSocket and REST endpoints to the Go backend.
      { source: '/twilio/:path*',        destination: `${backendURL}/twilio/:path*` },
      { source: '/browser/:path*',       destination: `${backendURL}/browser/:path*` },
      { source: '/events',               destination: `${backendURL}/events` },
      { source: '/api/:path*',           destination: `${backendURL}/api/:path*` },
    ];
  },
};

module.exports = nextConfig;
