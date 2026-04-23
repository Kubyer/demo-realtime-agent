/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      // Proxy WebSocket endpoints to the Go backend during development.
      {
        source: '/twilio/:path*',
        destination: 'http://localhost:8080/twilio/:path*',
      },
      {
        source: '/events',
        destination: 'http://localhost:8080/events',
      },
    ];
  },
};

module.exports = nextConfig;
