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
  getMaintenanceSchedules: () => Promise.resolve({
    timezone: 'host_local',
    update: { enabled: false, frequency: 'weekly', time: '03:30', weekdays: ['sat'], target: { mode: 'all' }, options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false }, status: {} },
    prune: { enabled: false, frequency: 'weekly', time: '04:30', weekdays: ['sun'], scope: { images: true, build_cache: true, stopped_containers: true, volumes: false }, status: {} },
  }),
  updateMaintenanceSchedules: vi.fn(),
}));

vi.mock("@/hooks/use-job-drawer", () => ({
  useJobDrawer: () => ({ openJob: vi.fn(), closeJob: vi.fn(), jobId: null }),
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
        post_update_recovery_failed: false,
        stacklab_service_error: false,
        runtime_health_degraded: false,
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
        post_update_recovery_failed: false,
        stacklab_service_error: true,
        runtime_health_degraded: true,
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
    fireEvent.click(screen.getByText("A stack becomes unhealthy or enters a restart loop"));
    fireEvent.click(screen.getByText("Stacklab itself starts logging errors"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(mockUpdateNotificationSettings).toHaveBeenCalledWith(
        expect.objectContaining({
          enabled: true,
          webhook_url: "https://hooks.example.test/stacklab",
          events: expect.objectContaining({
            job_failed: true,
            job_succeeded_with_warnings: true,
            maintenance_succeeded: true,
            stacklab_service_error: true,
            runtime_health_degraded: true,
          }),
        }),
      );
    });

    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("sends webhook test notification with current draft values", async () => {
    mockGetNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "https://hooks.example.test/saved",
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
        post_update_recovery_failed: false,
        stacklab_service_error: true,
        runtime_health_degraded: true,
      },
    });
    mockSendNotificationTest.mockResolvedValue({ sent: true, channel: 'webhook' });

    render(<SettingsPage />);
    await screen.findByText("Notifications");

    fireEvent.click(screen.getByLabelText("Enable notifications"));
    fireEvent.change(
      screen.getByPlaceholderText("https://hooks.example.com/stacklab"),
      {
        target: { value: "https://hooks.example.test/draft" },
      },
    );
    // Click the first "Send test" (webhook card)
    const sendTestButtons = screen.getAllByText("Send test");
    fireEvent.click(sendTestButtons[0]);

    await waitFor(() => {
      expect(mockSendNotificationTest).toHaveBeenCalledWith(
        expect.objectContaining({
          channel: 'webhook',
          webhook_url: "https://hooks.example.test/draft",
          events: expect.objectContaining({
            stacklab_service_error: true,
            runtime_health_degraded: true,
          }),
        }),
      );
    });
  });
});
