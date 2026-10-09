import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  experimental: {
    serverActions: {
      // localhost — только в dev: в проде он лишь расширял список источников,
      // которым Server Actions доверяют по заголовку Origin.
      allowedOrigins: [
        ...(process.env.NODE_ENV === "development" ? ["localhost:3000"] : []),
        ...(process.env.ALLOWED_ORIGINS?.split(",").map((o) => o.trim()).filter(Boolean) ?? []),
      ],
    },
  },
};

export default nextConfig;
