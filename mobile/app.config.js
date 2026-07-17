// Injects the Apple Team ID (needed to code-sign the widget extension on a
// physical device) from the environment. Simulator builds work unsigned, so
// this is optional locally — set EXPO_APPLE_TEAM_ID for device builds.
module.exports = ({ config }) => ({
  ...config,
  ios: {
    ...config.ios,
    ...(process.env.EXPO_APPLE_TEAM_ID
      ? { appleTeamId: process.env.EXPO_APPLE_TEAM_ID }
      : {}),
  },
});
