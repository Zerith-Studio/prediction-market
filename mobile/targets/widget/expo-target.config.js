/** @type {import('@bacons/apple-targets/app.plugin').Config} */
module.exports = (config) => ({
  type: "widget",
  name: "widget",
  displayName: "PitchMarket",
  bundleIdentifier: ".widget", // → com.pitchmarket.app.widget
  deploymentTarget: "17.5", // matches the app's expo-build-properties target
  frameworks: ["SwiftUI", "WidgetKit"],
  colors: {
    $widgetBackground: "#0a0a0b",
    $accent: "#34d399",
  },
  entitlements: {
    "com.apple.security.application-groups":
      config.ios.entitlements["com.apple.security.application-groups"],
  },
});
