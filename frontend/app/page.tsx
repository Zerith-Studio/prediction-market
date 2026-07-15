import { redirect } from "next/navigation";
import { DEMO_MARKET_ID } from "@/lib/fixtures";

// Root goes straight to the demo market for this pass; a markets index is its
// own craft.
export default function Home() {
  redirect(`/market/${DEMO_MARKET_ID}`);
}
