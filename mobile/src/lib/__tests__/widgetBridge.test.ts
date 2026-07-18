import { syncWidgetState, APP_GROUP } from "../widgetBridge";

test("app group id is pinned", () => {
  expect(APP_GROUP).toBe("group.com.pitchmarket.app");
});

test("syncWidgetState never throws when the native module is unavailable", () => {
  expect(() => syncWidgetState("8x9yK3mP2qR5tV7wA1bC4dE6fG8hJ9kL2mN4pQ6rS8t")).not.toThrow();
  expect(() => syncWidgetState(null)).not.toThrow();
});
