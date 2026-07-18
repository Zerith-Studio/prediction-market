// Pushes the widget extension's config into the shared App Group and asks
// WidgetKit to reload. iOS-only: ExtensionStorage is a native module that
// exists only in dev-client/release iOS builds — every call is guarded so
// Expo Go, Android, web, and jest all no-op instead of crashing.
import { Platform } from "react-native";

export const APP_GROUP = "group.com.pitchmarket.app";

export function syncWidgetState(wallet: string | null): void {
  if (Platform.OS !== "ios") return;
  try {
    // Lazy require, same pattern as the Privy backend in wallet.tsx.
    const { ExtensionStorage } = require("@bacons/apple-targets");
    const storage = new ExtensionStorage(APP_GROUP);
    const apiUrl = process.env.EXPO_PUBLIC_API_URL ?? "";
    if (wallet && apiUrl) {
      storage.set("wallet", wallet);
      storage.set("apiUrl", apiUrl);
    } else {
      storage.remove("wallet");
    }
    ExtensionStorage.reloadWidget();
  } catch {
    // Native module absent (Expo Go / simulator without the target / jest).
  }
}
