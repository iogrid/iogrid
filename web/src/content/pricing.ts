export interface PricingTier {
  id: string;
  name: string;
  price: string;
  unit: string;
  description: string;
  features: string[];
  cta: { href: string; label: string };
  highlight?: boolean;
}

export const customerPricing: PricingTier[] = [
  {
    id: "proxy",
    name: "Bandwidth proxy",
    price: "$0.40",
    unit: "per GB",
    description:
      "Residential IPs with cryptographic audit. 95% pool average; geo-targeting on every request.",
    features: [
      "Residential IPs across 195+ countries",
      "Per-byte category labels in audit log",
      "Session stickiness up to 30 minutes",
      "Geo-targeted at country and city level",
      "SOCKS5 + HTTP CONNECT",
      "Volume discounts above 500 GB / month",
    ],
    cta: { href: "/proxy", label: "Start with proxy" },
  },
  {
    id: "ios-build",
    name: "iOS build CI",
    price: "$0.04",
    unit: "per minute",
    description:
      "Pay-per-minute Mac CI. No 24-hour leases. No idle waste. Bring your Xcode project.",
    features: [
      "Ephemeral macOS VMs via Tart",
      "Latest 3 Xcode versions; older on request",
      "Apple Silicon (M1, M2, M3) providers",
      "S3 artifact bucket included",
      "GitHub Actions runner image available",
      "No minimum spend",
    ],
    cta: { href: "/ios-build", label: "Run a build" },
    highlight: true,
  },
  {
    id: "compute",
    name: "Docker compute",
    price: "$0.018",
    unit: "per vCPU-hour",
    description:
      "Linux Docker workloads on idle home + Mac hardware. gVisor-isolated. Cheaper than spot.",
    features: [
      "Any OCI image",
      "x86_64 and ARM64 providers",
      "gVisor or Kata Container isolation",
      "Up to 16 GB RAM per container",
      "Bandwidth included up to 50 GB / job",
      "Bring-your-registry credentials",
    ],
    cta: { href: "/compute", label: "Submit a container" },
  },
  {
    id: "gpu",
    name: "GPU inference",
    price: "$0.20",
    unit: "per GPU-hour",
    description:
      "Consumer GPUs (4090, 5090, Apple Silicon MLX). For batch inference and fine-tuning.",
    features: [
      "NVIDIA consumer cards (24 GB+ VRAM)",
      "Apple Silicon MLX (M3 Max, M4)",
      "Per-second billing after first minute",
      "Hugging Face TGI / vLLM templates",
      "Bring your own model weights",
      "Pre-flight benchmark before charge",
    ],
    cta: { href: "/gpu", label: "Run inference" },
  },
];

export const vpnPricing: PricingTier[] = [
  {
    id: "free",
    name: "Free",
    price: "$0",
    unit: "forever",
    description:
      "Free consumer VPN funded by bandwidth swap. You contribute a little capacity; you get a VPN.",
    features: [
      "Unlimited bandwidth on best-effort tier",
      "10+ countries available",
      "WireGuard tunnel",
      "Bandwidth swap is opt-in and transparent",
      "Block any category you don't want carrying through your IP",
    ],
    cta: { href: "/vpn", label: "Download free VPN" },
  },
  {
    id: "plus",
    name: "Plus",
    price: "$2.99",
    unit: "per month",
    description:
      "No bandwidth swap. Priority routing. Streaming-friendly servers.",
    features: [
      "No bandwidth contribution required",
      "Priority server pool",
      "60+ countries",
      "Stream-friendly residential exit nodes",
      "Up to 5 devices",
    ],
    cta: { href: "/vpn", label: "Upgrade to Plus" },
    highlight: true,
  },
  {
    id: "pro",
    name: "Pro",
    price: "$4.99",
    unit: "per month",
    description:
      "Maximum privacy. Multi-hop routing. Static IP option. Up to 10 devices.",
    features: [
      "Multi-hop routing (2 or 3 nodes)",
      "Static residential IP option",
      "Kill-switch + DNS leak protection",
      "Port-forwarding on request",
      "Up to 10 devices",
    ],
    cta: { href: "/vpn", label: "Upgrade to Pro" },
  },
];
