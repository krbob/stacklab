import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./settings-page";

const mockGetMeta = vi.fn();
const mockChangePassword = vi.fn();
const mockGetNotificationSettings = vi.fn();
const mockUpdateNotificationSettings = vi.fn();
const mockSendNotificationTest = vi.fn();
const mockGetMaintenanceSchedules = vi.fn();
const mockUpdateMaintenanceSchedules = vi.fn();
const mockGetStacks = vi.fn();

vi.mock("@/lib/api-client", () => ({
  getMeta: () => mockGetMeta(),
  changePassword: (...args: unknown[]) => mockChangePassword(...args),
  getNotificationSettings: () => mockGetNotificationSettings(),
  updateNotificationSettings: (...args: unknown[]) =>
    mockUpdateNotificationSettings(...args),
  sendNotificationTest: (...args: unknown[]) =>
    mockSendNotificationTest(...args),
  getMaintenanceSchedules: () => mockGetMaintenanceSchedules(),
  updateMaintenanceSchedules: (...args: unknown[]) =>
    mockUpdateMaintenanceSchedules(...args),
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
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
    mockGetMaintenanceSchedules.mockResolvedValue({
      timezone: 'host_local',
      update: { enabled: false, frequency: 'weekly', time: '03:30', weekdays: ['sat'], target: { mode: 'all' }, options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false }, status: {} },
      prune: { enabled: false, frequency: 'weekly', time: '04:30', weekdays: ['sun'], scope: { images: true, build_cache: true, stopped_containers: true, volumes: false }, status: {} },
    });
    mockUpdateMaintenanceSchedules.mockReset();
    mockGetStacks.mockResolvedValue({
      items: [
        {
          id: 'demo',
          name: 'demo',
          display_state: 'running',
          runtime_state: 'running',
          config_state: 'in_sync',
          activity_state: 'idle',
          health_summary: { healthy_container_count: 1, unhealthy_container_count: 0, unknown_health_container_count: 0 },
          service_count: { defined: 1, running: 1 },
          last_action: null,
        },
      ],
      summary: {
        stack_count: 1,
        running_count: 1,
        stopped_count: 0,
        error_count: 0,
        container_count: { running: 1, total: 1 },
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

  it("saves maintenance schedule with selected stacks", async () => {
    mockUpdateMaintenanceSchedules.mockResolvedValue({
      timezone: 'host_local',
      update: {
        enabled: true,
        frequency: 'weekly',
        time: '03:30',
        weekdays: ['sat'],
        target: { mode: 'selected', stack_ids: ['demo'] },
        options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false },
        status: {},
      },
      prune: {
        enabled: false,
        frequency: 'weekly',
        time: '04:30',
        weekdays: ['sun'],
        scope: { images: true, build_cache: true, stopped_containers: true, volumes: false },
        status: {},
      },
    });

    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getByLabelText("Scheduled stack update"));
    fireEvent.click(screen.getByLabelText("Selected stacks"));
    fireEvent.click(screen.getByLabelText("demo"));
    fireEvent.click(screen.getByText("Save schedules"));

    await waitFor(() => {
      expect(mockUpdateMaintenanceSchedules).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            target: { mode: 'selected', stack_ids: ['demo'] },
          }),
        }),
      );
    });
  });

  it("shows validation error when selected stacks is empty", async () => {
    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getByLabelText("Selected stacks"));
    fireEvent.click(screen.getByText("Save schedules"));

    expect(await screen.findByText("Select at least one stack for scheduled updates")).toBeInTheDocument();
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();
  });
});
