const { getDefaultConfig } = require("expo/metro-config");
const { withNativeWind } = require("nativewind/metro");
const config = getDefaultConfig(__dirname);
// Privy's transitive dep `jose` has no react-native export condition; without
// "browser" here Metro resolves its Node build (needs node:buffer/crypto) and
// the native bundle fails. Per Privy's React Native setup docs.
config.resolver.unstable_conditionNames = ["browser", "require", "react-native"];
module.exports = withNativeWind(config, { input: "./src/global.css" });
