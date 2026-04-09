import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./settings-page";

const mockGetMeta = vi.fn();
const mockChangePassword = vi.fn();
const mockGetNotificationSettings = vi.fn();
const mockUpdateNotificationSettings = vi.fn();
const mockSendNotificationTest = vi.fn();

vi.mock("@/lib/api-client", () => ({
  getMeta: () => mockGetMeta(),
  changePassword: (...args: unknown[]) => mockChangePassword(...args),
  getNotificationSettings: () => mockGetNotificationSettings(),
  updateNotificationSettings: (...args: unknown[]) =>
    mockUpdateNotificationSettings(...args),
  sendNotificationTest: (...args: unknown[]) =>
    mockSendNotificationTest(...args),
}));

describe("SettingsPage", () => {
  beforeEach(() => {
    mockGetMeta.mockReset();
    mockChangePassword.mockReset();
    mockGetNotificationSettings.mockReset();
    mockUpdateNotificationSettings.mockReset();
    mockSendNotificationTest.mockReset();

    mockGetMeta.mockResolvedValue({
      app: { name: "Stacklab", version: "0.1.0-dev" },
      environment: { stack_root: "/opt/stacklab", platform: "linux/amd64" },
      docker: { engine_version: "29.3.1", compose_version: "5.1.1" },
      features: { host_shell: false },
    });
    mockGetNotificationSettings.mockResolvedValue({
      enabled: false,
      configured: false,
      webhook_url: "",
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
      },
    });
  });

  it("renders notifications section with loaded settings", async () => {
    render(<SettingsPage />);

    expect(await screen.findByText("Notifications")).toBeInTheDocument();
    expect(screen.getByLabelText("Enable notifications")).not.toBeChecked();
    expect(screen.getByText("Failed jobs")).toBeInTheDocument();
    expect(screen.getByText("Succeeded with warnings")).toBeInTheDocument();
  });

  it("saves notification settings", async () => {
    mockUpdateNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "https://hooks.example.test/stacklab",
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: true,
      },
    });

    render(<SettingsPage />);
    await screen.findByText("Notifications");

    fireEvent.click(screen.getByLabelText("Enable notifications"));
    fireEvent.change(
      screen.getByPlaceholderText("https://hooks.example.com/stacklab"),
      {
        target: { value: "https://hooks.example.test/stacklab" },
      },
    );
    fireEvent.click(screen.getByText("Maintenance succeeded"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(mockUpdateNotificationSettings).toHaveBeenCalledWith({
        enabled: true,
        webhook_url: "https://hooks.example.test/stacklab",
        events: {
          job_failed: true,
          job_succeeded_with_warnings: true,
          maintenance_succeeded: true,
        },
      });
    });

    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("sends test notification with current draft values", async () => {
    mockGetNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "https://hooks.example.test/saved",
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
      },
    });
    mockSendNotificationTest.mockResolvedValue({ sent: true });

    render(<SettingsPage />);
    await screen.findByText("Notifications");

    fireEvent.click(screen.getByLabelText("Enable notifications"));
    fireEvent.change(
      screen.getByPlaceholderText("https://hooks.example.com/stacklab"),
      {
        target: { value: "https://hooks.example.test/draft" },
      },
    );
    fireEvent.click(screen.getByText("Send test"));

    await waitFor(() => {
      expect(mockSendNotificationTest).toHaveBeenCalledWith({
        enabled: false,
        webhook_url: "https://hooks.example.test/draft",
        events: {
          job_failed: true,
          job_succeeded_with_warnings: true,
          maintenance_succeeded: false,
        },
      });
    });
  });
});
