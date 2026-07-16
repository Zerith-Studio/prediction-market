import "react-native-get-random-values";
import "../global.css";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { PitchWalletProvider } from "@/lib/wallet";

export default function RootLayout() {
  return (
    <PitchWalletProvider>
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
