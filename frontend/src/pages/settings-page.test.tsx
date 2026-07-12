import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsPage } from "./settings-page";
import { useAuth } from "@/hooks/use-auth";
import type { NotificationSettingsResponse } from "@/lib/api-types";

const mockGetMeta = vi.fn();
const mockChangePassword = vi.fn();
const mockGetNotificationSettings = vi.fn();
const mockUpdateNotificationSettings = vi.fn();
const mockSendNotificationTest = vi.fn();
const mockGetMaintenanceSchedules = vi.fn();
const mockUpdateMaintenanceSchedules = vi.fn();
const mockGetHostSettings = vi.fn();
const mockUpdateHostSettings = vi.fn();
const mockGetStacks = vi.fn();
const mockGetStack = vi.fn();
const mockGetStacklabUpdateOverview = vi.fn();
const mockApplyStacklabUpdate = vi.fn();
const mockOpenJob = vi.fn();
const mockRequireReauthentication = vi.fn();

vi.mock("@/hooks/use-auth", () => ({
  useAuth: vi.fn(),
}));

const mockUseAuth = vi.mocked(useAuth);

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
  getHostSettings: () => mockGetHostSettings(),
  updateHostSettings: (...args: unknown[]) => mockUpdateHostSettings(...args),
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
  getStack: (...args: unknown[]) => mockGetStack(...args),
  getStacklabUpdateOverview: () => mockGetStacklabUpdateOverview(),
  applyStacklabUpdate: (...args: unknown[]) => mockApplyStacklabUpdate(...args),
}));

vi.mock("@/hooks/use-job-drawer", () => ({
  useJobDrawer: () => ({ openJob: mockOpenJob, closeJob: vi.fn(), jobId: null }),
}));

describe("SettingsPage", () => {
  beforeEach(() => {
    vi.useRealTimers();
    mockGetMeta.mockReset();
    mockChangePassword.mockReset();
    mockGetNotificationSettings.mockReset();
    mockUpdateNotificationSettings.mockReset();
    mockSendNotificationTest.mockReset();
    mockGetMaintenanceSchedules.mockReset();
    mockGetStacks.mockReset();
    mockGetHostSettings.mockReset();
    mockUpdateHostSettings.mockReset();
    mockGetStacklabUpdateOverview.mockReset();
    mockApplyStacklabUpdate.mockReset();
    mockGetStack.mockReset();
    mockOpenJob.mockReset();
    mockRequireReauthentication.mockReset();
    mockUseAuth.mockReturnValue({
      status: "authenticated",
      session: null,
      login: vi.fn(),
      logout: vi.fn(),
      requireReauthentication: mockRequireReauthentication,
    });

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
      channels: {
        webhook: { enabled: false, configured: false, url: "" },
        telegram: { enabled: false, configured: false, bot_token_configured: false, chat_id: "" },
      },
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
        post_update_recovery_failed: false,
        stacklab_service_error: false,
        runtime_health_degraded: false,
        runtime_log_error_burst: false,
      },
    } satisfies NotificationSettingsResponse);
    mockGetMaintenanceSchedules.mockResolvedValue({
      timezone: 'host_local',
      update: { enabled: false, frequency: 'weekly', time: '03:30', weekdays: ['sat'], target: { mode: 'all' }, options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false }, status: {} },
      prune: { enabled: false, frequency: 'weekly', time: '04:30', weekdays: ['sun'], scope: { images: true, build_cache: true, stopped_containers: true, volumes: false }, status: {} },
    });
    mockUpdateMaintenanceSchedules.mockReset();
    mockGetHostSettings.mockResolvedValue({
      public_ip_lookup_enabled: false,
    });
    mockUpdateHostSettings.mockResolvedValue({
      public_ip_lookup_enabled: true,
    });
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
    mockGetStack.mockResolvedValue({
      stack: {
        id: 'demo',
        name: 'demo',
        display_state: 'running',
        runtime_state: 'running',
        config_state: 'in_sync',
        activity_state: 'idle',
        health_summary: { healthy_container_count: 1, unhealthy_container_count: 0, unknown_health_container_count: 0 },
        capabilities: { can_edit_definition: true, can_view_logs: true, can_view_stats: true, can_open_terminal: true },
        available_actions: ['up'],
        services: [{ name: 'app', mode: 'image', healthcheck_present: true }],
        containers: [],
      },
    });
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: "2026.04.0",
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.0",
        configured_channel: "stable",
        update_available: false,
      },
      write_capability: {
        supported: true,
      },
    });
  });

  afterEach(() => {
    if (vi.isFakeTimers()) {
      vi.clearAllTimers();
    }
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("requires a fresh login after changing the password", async () => {
    mockChangePassword.mockResolvedValue({ updated: true, reauthentication_required: true });
    render(<SettingsPage />);

    const passwordCard = screen.getByText("Change password").closest("section") ?? document.body;
    fireEvent.change(within(passwordCard).getByPlaceholderText("Current password"), { target: { value: "secret" } });
    fireEvent.change(within(passwordCard).getByPlaceholderText("New password"), { target: { value: "new-test-password" } });
    fireEvent.change(within(passwordCard).getByPlaceholderText("Confirm new password"), { target: { value: "new-test-password" } });
    fireEvent.click(within(passwordCard).getByRole("button", { name: "Update password" }));

    await waitFor(() => expect(mockRequireReauthentication).toHaveBeenCalledWith("password_changed"));
  });

  it("rejects a new password outside the supported length", () => {
    render(<SettingsPage />);

    const passwordCard = screen.getByText("Change password").closest("section") ?? document.body;
    fireEvent.change(within(passwordCard).getByPlaceholderText("Current password"), { target: { value: "test-password" } });
    for (const invalidPassword of ["too-short", "x".repeat(257)]) {
      fireEvent.change(within(passwordCard).getByPlaceholderText("New password"), { target: { value: invalidPassword } });
      fireEvent.change(within(passwordCard).getByPlaceholderText("Confirm new password"), { target: { value: invalidPassword } });
      fireEvent.click(within(passwordCard).getByRole("button", { name: "Update password" }));
      expect(within(passwordCard).getByText("Password must contain between 12 and 256 characters")).toBeInTheDocument();
    }
    expect(mockChangePassword).not.toHaveBeenCalled();
  });

  it("renders notifications section with loaded settings", async () => {
    render(<SettingsPage />);

    expect(await screen.findByText("Notifications")).toBeInTheDocument();
    expect(screen.getByLabelText("Enable notifications")).not.toBeChecked();
    expect(screen.getByText("Failed jobs")).toBeInTheDocument();
    expect(screen.getByText("Succeeded with warnings")).toBeInTheDocument();
  });

  it("blocks notification edits when loading settings fails", async () => {
    mockGetNotificationSettings.mockRejectedValue(new Error("notification load failed"));

    render(<SettingsPage />);

    expect(await screen.findByText("notification load failed")).toBeInTheDocument();
    expect(screen.queryByLabelText("Enable notifications")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("https://hooks.example.com/stacklab")).not.toBeInTheDocument();
    expect(mockUpdateNotificationSettings).not.toHaveBeenCalled();
  });

  it("retries only the settings endpoint that failed", async () => {
    mockGetNotificationSettings.mockRejectedValueOnce(new Error("notification load failed"));

    render(<SettingsPage />);

    expect(await screen.findByText("notification load failed")).toBeInTheDocument();
    expect(await screen.findByText("Maintenance schedules")).toBeInTheDocument();
    expect(await screen.findByText("Host observability")).toBeInTheDocument();
    await waitFor(() => {
      expect(mockGetMeta).toHaveBeenCalledTimes(1);
      expect(mockGetNotificationSettings).toHaveBeenCalledTimes(1);
      expect(mockGetMaintenanceSchedules).toHaveBeenCalledTimes(1);
      expect(mockGetStacks).toHaveBeenCalledTimes(1);
      expect(mockGetHostSettings).toHaveBeenCalledTimes(1);
      expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByLabelText("Enable notifications")).toBeInTheDocument();
    expect(mockGetNotificationSettings).toHaveBeenCalledTimes(2);
    expect(mockGetMeta).toHaveBeenCalledTimes(1);
    expect(mockGetMaintenanceSchedules).toHaveBeenCalledTimes(1);
    expect(mockGetStacks).toHaveBeenCalledTimes(1);
    expect(mockGetHostSettings).toHaveBeenCalledTimes(1);
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);
  });

  it("saves notification settings", async () => {
    mockUpdateNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "https://hooks.example.test/stacklab",
      channels: {
        webhook: { enabled: true, configured: true, url: "https://hooks.example.test/stacklab" },
        telegram: { enabled: false, configured: false, bot_token_configured: false, chat_id: "" },
      },
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: true,
        post_update_recovery_failed: false,
        stacklab_service_error: true,
        runtime_health_degraded: true,
        runtime_log_error_burst: true,
      },
    } satisfies NotificationSettingsResponse);

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
    fireEvent.click(screen.getByText("A stack starts logging repeated errors"));
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
            runtime_log_error_burst: true,
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
      channels: {
        webhook: { enabled: true, configured: true, url: "https://hooks.example.test/saved" },
        telegram: { enabled: false, configured: false, bot_token_configured: false, chat_id: "" },
      },
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
        post_update_recovery_failed: false,
        stacklab_service_error: true,
        runtime_health_degraded: true,
        runtime_log_error_burst: true,
      },
    } satisfies NotificationSettingsResponse);
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
            runtime_log_error_burst: true,
          }),
        }),
      );
    });
  });

  it("keeps a configured Telegram token when its input stays empty", async () => {
    mockGetNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "",
      channels: {
        webhook: { enabled: false, configured: false, url: "" },
        telegram: {
          enabled: true,
          configured: true,
          bot_token_configured: true,
          chat_id: "-1001234567890",
        },
      },
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: false,
        post_update_recovery_failed: false,
        stacklab_service_error: false,
        runtime_health_degraded: false,
        runtime_log_error_burst: false,
      },
    } satisfies NotificationSettingsResponse);
    mockUpdateNotificationSettings.mockResolvedValue({
      enabled: true,
      configured: true,
      webhook_url: "",
      channels: {
        webhook: { enabled: false, configured: false, url: "" },
        telegram: { enabled: true, configured: true, bot_token_configured: true, chat_id: "-1001234567890" },
      },
      events: {
        job_failed: true,
        job_succeeded_with_warnings: true,
        maintenance_succeeded: true,
        post_update_recovery_failed: false,
        stacklab_service_error: false,
        runtime_health_degraded: false,
        runtime_log_error_burst: false,
      },
    } satisfies NotificationSettingsResponse);

    render(<SettingsPage />);

    expect(await screen.findByText("(configured)")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("(leave empty to keep current)")).toHaveValue("");
    fireEvent.click(screen.getByText("Maintenance succeeded"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockUpdateNotificationSettings).toHaveBeenCalledWith(
        expect.objectContaining({
          channels: expect.objectContaining({
            telegram: {
              enabled: true,
              bot_token: "",
              chat_id: "-1001234567890",
            },
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

  it("requires a review before scheduling unused volume deletion", async () => {
    mockUpdateMaintenanceSchedules.mockResolvedValue({
      timezone: 'host_local',
      update: {
        enabled: false,
        frequency: 'weekly',
        time: '03:30',
        weekdays: ['sat'],
        target: { mode: 'all' },
        options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false },
        status: {},
      },
      prune: {
        enabled: true,
        frequency: 'weekly',
        time: '04:30',
        weekdays: ['sun'],
        scope: { images: true, build_cache: true, stopped_containers: true, volumes: true },
        status: {},
      },
    });

    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getByLabelText("Scheduled cleanup"));
    fireEvent.click(screen.getByLabelText("Unused volumes"));
    fireEvent.click(screen.getByText("Save schedules"));

    const dialog = screen.getByRole("dialog", { name: "Enable scheduled volume deletion?" });
    const review = within(dialog).getByRole("region", { name: "Review operation" });
    expect(review).toHaveTextContent("Unused external Docker volumes on this host");
    expect(review).toHaveTextContent("Scheduled cleanup: weekly on Sun at 04:30");
    expect(review).toHaveTextContent("stack-managed volumes are excluded");
    expect(review).toHaveTextContent("runs automatically without another confirmation");
    expect(review).toHaveTextContent("does not create automatic volume snapshots or backups");
    expect(review).toHaveTextContent("Disabling the schedule prevents future runs but cannot undo completed cleanup");
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Save volume cleanup" }));
    await waitFor(() => {
      expect(mockUpdateMaintenanceSchedules).toHaveBeenCalledWith(
        expect.objectContaining({
          prune: expect.objectContaining({
            enabled: true,
            scope: expect.objectContaining({ volumes: true }),
          }),
        }),
      );
    });
  });

  it("does not save scheduled volume deletion when the review is cancelled", async () => {
    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getByLabelText("Scheduled cleanup"));
    fireEvent.click(screen.getByLabelText("Unused volumes"));
    fireEvent.click(screen.getByText("Save schedules"));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();
  });

  it("blocks maintenance schedule edits when loading schedules fails", async () => {
    mockGetMaintenanceSchedules.mockRejectedValue(new Error("schedule load failed"));

    render(<SettingsPage />);

    expect(await screen.findByText("schedule load failed")).toBeInTheDocument();
    expect(screen.queryByLabelText("Scheduled stack update")).not.toBeInTheDocument();
    expect(screen.queryByText("Save schedules")).not.toBeInTheDocument();
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();
  });

  it("saves host observability settings", async () => {
    render(<SettingsPage />);

    expect(await screen.findByText("Host observability")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Enable public IP lookup"));
    fireEvent.click(screen.getByText("Save host settings"));

    await waitFor(() => {
      expect(mockUpdateHostSettings).toHaveBeenCalledWith({
        public_ip_lookup_enabled: true,
      });
    });
    expect(await screen.findByText("Saved")).toBeInTheDocument();
  });

  it("blocks host observability edits when loading settings fails", async () => {
    mockGetHostSettings.mockRejectedValue(new Error("host load failed"));

    render(<SettingsPage />);

    expect(await screen.findByText("host load failed")).toBeInTheDocument();
    expect(screen.queryByLabelText("Enable public IP lookup")).not.toBeInTheDocument();
    expect(screen.queryByText("Save host settings")).not.toBeInTheDocument();
    expect(mockUpdateHostSettings).not.toHaveBeenCalled();
  });

  it("lazy-loads stack services only after expanding skip services", async () => {
    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    await waitFor(() => {
      expect(mockGetStacks).toHaveBeenCalled();
    });
    expect(mockGetStack).not.toHaveBeenCalled();

    fireEvent.click(screen.getByLabelText("Show services for demo"));

    await waitFor(() => {
      expect(mockGetStack).toHaveBeenCalledWith("demo");
    });
    expect(await screen.findByText("app")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Hide services for demo"));
    fireEvent.click(screen.getByLabelText("Show services for demo"));

    expect(screen.getByText("app")).toBeInTheDocument();
    expect(mockGetStack).toHaveBeenCalledTimes(1);
  });

  it("retries a failed stack service load without showing a false empty state", async () => {
    mockGetStack.mockRejectedValueOnce(new Error("stack detail unavailable"));

    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");
    fireEvent.click(screen.getByLabelText("Show services for demo"));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Failed to load services: stack detail unavailable");
    expect(screen.queryByText("No services.")).not.toBeInTheDocument();

    fireEvent.click(within(alert).getByRole("button", { name: "Retry services for demo" }));

    expect(await screen.findByLabelText("app")).toBeInTheDocument();
    expect(mockGetStack).toHaveBeenCalledTimes(2);
  });

  it("shows No services only after a successful empty stack detail response", async () => {
    mockGetStack.mockResolvedValueOnce({
      stack: { id: "demo", services: [] },
    });

    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");
    fireEvent.click(screen.getByLabelText("Show services for demo"));

    expect(await screen.findByText("No services.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("hydrates persisted service exclusions and normalizes them on save", async () => {
    mockGetMaintenanceSchedules.mockResolvedValue({
      timezone: "host_local",
      update: {
        enabled: true,
        frequency: "weekly",
        time: "03:30",
        weekdays: ["sat"],
        target: {
          mode: "selected",
          stack_ids: ["demo"],
          excluded_services: {
            demo: ["worker", "app", "worker", ""],
            removed: ["legacy"],
          },
        },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: false,
          prune_after: false,
          include_volumes: false,
        },
        status: {},
      },
      prune: {
        enabled: false,
        frequency: "weekly",
        time: "04:30",
        weekdays: ["sun"],
        scope: {
          images: true,
          build_cache: true,
          stopped_containers: true,
          volumes: false,
        },
        status: {},
      },
    });
    mockGetStack.mockResolvedValue({
      stack: {
        id: "demo",
        name: "demo",
        display_state: "running",
        runtime_state: "running",
        config_state: "in_sync",
        activity_state: "idle",
        health_summary: {
          healthy_container_count: 2,
          unhealthy_container_count: 0,
          unknown_health_container_count: 0,
        },
        capabilities: {
          can_edit_definition: true,
          can_view_logs: true,
          can_view_stats: true,
          can_open_terminal: true,
        },
        available_actions: ["up"],
        services: [
          { name: "worker", mode: "image", healthcheck_present: true },
          { name: "app", mode: "image", healthcheck_present: true },
        ],
        containers: [],
      },
    });
    mockUpdateMaintenanceSchedules.mockResolvedValue({
      timezone: "host_local",
      update: {
        enabled: true,
        frequency: "weekly",
        time: "03:30",
        weekdays: ["sat"],
        target: {
          mode: "selected",
          stack_ids: ["demo"],
          excluded_services: { demo: ["app", "worker"] },
        },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: false,
          prune_after: false,
          include_volumes: false,
        },
        status: {},
      },
      prune: {
        enabled: false,
        frequency: "weekly",
        time: "04:30",
        weekdays: ["sun"],
        scope: {
          images: true,
          build_cache: true,
          stopped_containers: true,
          volumes: false,
        },
        status: {},
      },
    });

    render(<SettingsPage />);

    await waitFor(() => {
      expect(mockGetStack).toHaveBeenCalledTimes(1);
      expect(mockGetStack).toHaveBeenCalledWith("demo");
    });
    expect(await screen.findByLabelText("app")).toBeChecked();
    expect(screen.getByLabelText("worker")).toBeChecked();

    fireEvent.click(screen.getByText("Save schedules"));

    await waitFor(() => {
      expect(mockUpdateMaintenanceSchedules).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            target: {
              mode: "selected",
              stack_ids: ["demo"],
              excluded_services: { demo: ["app", "worker"] },
            },
          }),
        }),
      );
    });
  });

  it("does not loop failed persisted service loads or drop their exclusions on save", async () => {
    const schedulesWithExclusion = {
      timezone: "host_local",
      update: {
        enabled: true,
        frequency: "weekly",
        time: "03:30",
        weekdays: ["sat"],
        target: {
          mode: "selected",
          stack_ids: ["demo"],
          excluded_services: { demo: ["app"] },
        },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: false,
          prune_after: false,
          include_volumes: false,
        },
        status: {},
      },
      prune: {
        enabled: false,
        frequency: "weekly",
        time: "04:30",
        weekdays: ["sun"],
        scope: {
          images: true,
          build_cache: true,
          stopped_containers: true,
          volumes: false,
        },
        status: {},
      },
    };
    mockGetMaintenanceSchedules.mockResolvedValue(schedulesWithExclusion);
    mockGetStack.mockRejectedValue(new Error("service lookup failed"));
    mockUpdateMaintenanceSchedules.mockResolvedValue(schedulesWithExclusion);

    render(<SettingsPage />);

    expect(await screen.findByRole("alert")).toHaveTextContent("service lookup failed");
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockGetStack).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("No services.")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("Save schedules"));

    await waitFor(() => {
      expect(mockUpdateMaintenanceSchedules).toHaveBeenCalledWith(
        expect.objectContaining({
          update: expect.objectContaining({
            target: {
              mode: "selected",
              stack_ids: ["demo"],
              excluded_services: { demo: ["app"] },
            },
          }),
        }),
      );
    });
    expect(mockGetStack).toHaveBeenCalledTimes(1);
  });

  it("shows validation error when selected stacks is empty", async () => {
    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getByLabelText("Selected stacks"));
    fireEvent.click(screen.getByText("Save schedules"));

    expect(await screen.findByText("Select at least one stack for scheduled updates")).toBeInTheDocument();
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();
  });

  it("shows validation error when a weekly schedule has no weekday", async () => {
    render(<SettingsPage />);
    await screen.findByText("Maintenance schedules");

    fireEvent.click(screen.getAllByRole("button", { name: "Sat" })[0]);
    fireEvent.click(screen.getByText("Save schedules"));

    expect(await screen.findByText("Select at least one weekday for scheduled updates")).toBeInTheDocument();
    expect(mockUpdateMaintenanceSchedules).not.toHaveBeenCalled();
  });

  it("retries only the failed Stacklab update overview", async () => {
    mockGetStacklabUpdateOverview
      .mockRejectedValueOnce(new Error("update overview unavailable"))
      .mockResolvedValueOnce({
        current_version: "2026.04.0",
        install_mode: "apt",
        package: {
          supported: true,
          name: "stacklab",
          installed_version: "2026.04.0",
          candidate_version: "2026.04.0",
          configured_channel: "stable",
          update_available: false,
        },
        write_capability: { supported: true },
      });

    render(<SettingsPage />);

    const updateCard = (await screen.findByText("Stacklab update")).closest("section") ?? document.body;
    expect(await within(updateCard).findByRole("alert")).toHaveTextContent("update overview unavailable");
    expect(within(updateCard).queryByRole("button", { name: "Update Stacklab" })).not.toBeInTheDocument();

    await waitFor(() => {
      expect(mockGetMeta).toHaveBeenCalledTimes(1);
      expect(mockGetNotificationSettings).toHaveBeenCalledTimes(1);
      expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);
    });
    fireEvent.click(within(updateCard).getByRole("button", { name: "Retry update status" }));

    expect(await within(updateCard).findByText("Stacklab is already up to date.")).toBeInTheDocument();
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(2);
    expect(mockGetMeta).toHaveBeenCalledTimes(1);
    expect(mockGetNotificationSettings).toHaveBeenCalledTimes(1);
  });

  it("preserves and disables the Stacklab update overview when its delayed refresh fails", async () => {
    mockGetStacklabUpdateOverview
      .mockResolvedValueOnce({
        current_version: "2026.04.0",
        install_mode: "apt",
        package: {
          supported: true,
          name: "stacklab",
          installed_version: "2026.04.0",
          candidate_version: "2026.04.1",
          configured_channel: "stable",
          update_available: true,
        },
        write_capability: { supported: true },
      })
      .mockRejectedValueOnce(new Error("service restarting"))
      .mockResolvedValueOnce({
        current_version: "2026.04.1",
        install_mode: "apt",
        package: {
          supported: true,
          name: "stacklab",
          installed_version: "2026.04.1",
          candidate_version: "2026.04.1",
          configured_channel: "stable",
          update_available: false,
        },
        write_capability: { supported: true },
      });
    mockApplyStacklabUpdate.mockResolvedValue({
      started: true,
      job: {
        id: "job_update",
        stack_id: null,
        action: "self_update_stacklab",
        state: "running",
      },
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
    });

    render(<SettingsPage />);
    expect(await screen.findByText("Update available: 2026.04.1")).toBeInTheDocument();
    const updateCard = screen.getByText("Stacklab update").closest("section") ?? document.body;

    vi.useFakeTimers();
    fireEvent.click(within(updateCard).getByRole("button", { name: "Update Stacklab" }));
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Update Stacklab?" })).getByRole("button", {
        name: "Update Stacklab",
      }),
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(within(updateCard).getByRole("status")).toHaveTextContent("Refreshing…");
    expect(within(updateCard).getByRole("button", { name: "Updating..." })).toBeDisabled();
    expect(mockApplyStacklabUpdate).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });

    const alert = within(updateCard).getByRole("alert");
    expect(alert).toHaveTextContent("service restarting");
    expect(alert).toHaveTextContent("Showing the last successfully loaded data.");
    expect(within(updateCard).getByText("Update available: 2026.04.1")).toBeInTheDocument();
    expect(within(updateCard).getByRole("button", { name: "Updating..." })).toBeDisabled();

    await act(async () => {
      fireEvent.click(within(updateCard).getByRole("button", { name: "Retry update status" }));
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(within(updateCard).getByText("Stacklab is already up to date.")).toBeInTheDocument();
    expect(within(updateCard).queryByRole("alert")).not.toBeInTheDocument();
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(3);
  });

  it("starts stacklab self-update and opens the job drawer", async () => {
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: "2026.04.0",
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
      write_capability: {
        supported: true,
      },
    });
    mockApplyStacklabUpdate.mockResolvedValue({
      started: true,
      job: {
        id: "job_update",
        stack_id: null,
        action: "self_update_stacklab",
        state: "running",
      },
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
    });

    render(<SettingsPage />);

    expect(await screen.findByText("Update available: 2026.04.1")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Update Stacklab"));
    const dialog = screen.getByRole("dialog", { name: "Update Stacklab?" });
    const review = within(dialog).getByRole("region", { name: "Review operation" });
    expect(review).toHaveTextContent("stacklab 2026.04.0 → 2026.04.1");
    expect(review).toHaveTextContent("Refresh the APT package index from the stable channel.");
    expect(review).toHaveTextContent("UI and API connections may disconnect briefly");
    expect(review).toHaveTextContent("No automatic package snapshot is created.");
    expect(review).toHaveTextContent("Rollback is manual");
    expect(mockApplyStacklabUpdate).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Update Stacklab" }));

    await waitFor(() => {
      expect(mockApplyStacklabUpdate).toHaveBeenCalledWith({
        expected_candidate_version: "2026.04.1",
        refresh_package_index: true,
      });
      expect(mockOpenJob).toHaveBeenCalledWith("job_update");
    });
  });

  it("refreshes self-update overview exactly once two seconds after a successful start", async () => {
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: "2026.04.0",
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
      write_capability: { supported: true },
    });
    mockApplyStacklabUpdate.mockResolvedValue({
      started: true,
      job: {
        id: "job_update",
        stack_id: null,
        action: "self_update_stacklab",
        state: "running",
      },
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
    });

    const { unmount } = render(<SettingsPage />);
    expect(await screen.findByText("Update available: 2026.04.1")).toBeInTheDocument();
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);

    vi.useFakeTimers();
    fireEvent.click(screen.getByText("Update Stacklab"));
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Update Stacklab?" })).getByRole("button", {
        name: "Update Stacklab",
      }),
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockApplyStacklabUpdate).toHaveBeenCalledTimes(1);
    const updateCard = screen.getByText("Stacklab update").closest("section") ?? document.body;
    expect(within(updateCard).getByRole("button", { name: "Updating..." })).toBeDisabled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_999);
    });
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(2);

    unmount();
  });

  it("cancels the delayed Stacklab update refresh when the section unmounts", async () => {
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: "2026.04.0",
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
      write_capability: { supported: true },
    });
    mockApplyStacklabUpdate.mockResolvedValue({
      started: true,
      job: {
        id: "job_update",
        stack_id: null,
        action: "self_update_stacklab",
        state: "running",
      },
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
    });

    const { unmount } = render(<SettingsPage />);
    expect(await screen.findByText("Update available: 2026.04.1")).toBeInTheDocument();

    vi.useFakeTimers();
    fireEvent.click(screen.getByText("Update Stacklab"));
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Update Stacklab?" })).getByRole("button", {
        name: "Update Stacklab",
      }),
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);

    unmount();
    await vi.advanceTimersByTimeAsync(2_000);

    expect(mockGetStacklabUpdateOverview).toHaveBeenCalledTimes(1);
  });

  it("allows long nightly self-update versions to wrap on mobile", async () => {
    const currentVersion = "2026.08.0~nightly20260707+r123.g0f60ce54verylongsuffix";
    const candidateVersion = "2026.08.0~nightly20260708+r124.gabcdef1234567890verylongsuffix";
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: currentVersion,
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: currentVersion,
        candidate_version: candidateVersion,
        configured_channel: "nightly",
        update_available: true,
      },
      write_capability: {
        supported: true,
      },
    });

    render(<SettingsPage />);

    const currentVersionNodes = await screen.findAllByText(currentVersion);
    expect(currentVersionNodes.some((node) => node.className.includes("break-all"))).toBe(true);
    expect(screen.getByText(candidateVersion).className).toContain("break-all");
    expect(screen.getByText(`Update available: ${candidateVersion}`).className).toContain("break-all");
  });

  it("keeps stacklab self-update disabled while runtime job is active", async () => {
    mockGetStacklabUpdateOverview.mockResolvedValue({
      current_version: "2026.04.0",
      install_mode: "apt",
      package: {
        supported: true,
        name: "stacklab",
        installed_version: "2026.04.0",
        candidate_version: "2026.04.1",
        configured_channel: "stable",
        update_available: true,
      },
      write_capability: {
        supported: true,
      },
      runtime: {
        job_id: "job_update",
        pending_finalize: false,
        requested_version: "2026.04.1",
        started_at: "2026-04-11T09:00:00Z",
      },
    });

    render(<SettingsPage />);

    expect(await screen.findByText("running")).toBeInTheDocument();
    expect(screen.getByText("Updating...")).toBeDisabled();
  });
});
