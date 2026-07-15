/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  webpack: (config) => {
    // Privy statically imports optional integrations we don't use — stub them
    // so webpack stops trying to resolve packages that aren't installed.
    config.resolve.alias = {
      ...config.resolve.alias,
      "@stripe/crypto": false,
      "@farcaster/mini-app-solana": false,
      "@abstract-foundation/agw-client": false,
      permissionless: false,
    };
    return config;
  },
};

export default nextConfig;
