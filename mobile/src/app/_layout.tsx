import "react-native-get-random-values";
import "../global.css";
import { useEffect } from "react";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { PitchWalletProvider, usePitchWallet } from "@/lib/wallet";
import { syncWidgetState } from "@/lib/widgetBridge";

// Mirrors the wallet address into the iOS widget App Group whenever it
// changes (connect/disconnect, either backend). Renders nothing.
function WidgetSync() {
  const wallet = usePitchWallet();
  useEffect(() => {
    if (wallet.ready) syncWidgetState(wallet.address);
  }, [wallet.ready, wallet.address]);
  return null;
}

export default function RootLayout() {
  return (
    <PitchWalletProvider>
      <WidgetSync />
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerShown: false,
          contentStyle: { backgroundColor: "#0a0a0b" },
        }}
      />
    </PitchWalletProvider>
  );
}
