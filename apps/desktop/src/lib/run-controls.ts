import type { APIClient, NetworkProfile, Run, StartInput } from "@router-forge/api";
import { agentReasons, environmentNetworkErrors, executionNetworkStages } from "./forms";

export type RunApprovals = { key: string | null; paid: boolean; executionTLSRisk: boolean; modelTLSRisk: boolean };
export const emptyRunApprovals = (): RunApprovals => ({ key: null, paid: false, executionTLSRisk: false, modelTLSRisk: false });

export function runConfirmationKey(run: Run): string {
  return JSON.stringify([run.id, run.state, run.snapshot]);
}

export function runTLSRisks(run: Run): { execution: boolean; model: boolean } {
  return { execution: executionNetworkStages.some(stage => run.snapshot.network?.[stage]?.tls_mode === "skip"), model: run.snapshot.network?.model?.tls_mode === "skip" };
}

export function startApprovalInput(run: Run, approvals: RunApprovals): StartInput {
  const current = approvals.key === runConfirmationKey(run);
  const risks = runTLSRisks(run);
  return { config_hash: run.snapshot.config_hash, confirm_paid: current && approvals.paid,
    ...(risks.execution ? { confirm_execution_tls_risk: current && approvals.executionTLSRisk } : {}),
    ...(risks.model ? { confirm_model_tls_risk: current && approvals.modelTLSRisk } : {}),
  };
}

export function legacyDraft(run: Run): boolean {
  return run.state === "draft" && (run.snapshot.format_version !== 3 || !run.snapshot.network);
}

function completeFrozenProfile(profile: NetworkProfile | undefined): boolean {
  return Boolean(profile?.id?.trim() && profile.revision?.trim())
    && (profile?.tls_mode === "verify" || profile?.tls_mode === "skip")
    && (profile.trust_mode === "system" || profile.trust_mode === "custom")
    && (profile.proxy_mode === "direct" || profile.proxy_mode === "proxy");
}

export function boundedDraft(run: Run): boolean {
  const { spec, suite, resolved_tasks, format_version, config_hash, resolution_hash, resolved_at, environment, network, model, provider } = run.snapshot;
  return run.state === "draft" && format_version === 3 && Boolean(network)
    && environment.id === spec.environment_id && model.id === spec.model_id && provider.id === model.provider_id
    && executionNetworkStages.every(stage => completeFrozenProfile(network?.[stage]) && network?.[stage].id === environment.networks?.[`${stage}_profile_id`] && network?.[stage].scope === "environment" && network?.[stage].environment_id === environment.id)
    && completeFrozenProfile(network?.model) && network?.model.scope === "desktop" && !network.model.environment_id && network.model.id === provider.network_profile_id
    && Object.keys(environmentNetworkErrors(environment, network ? Object.values(network) : [], true)).length === 0
    && spec.suite_id === "swebench-verified"
    && suite.id === spec.suite_id && suite.dataset === "swe-bench/swe-bench-verified"
    && spec.task_names.length === 1 && Boolean(spec.task_names[0])
    && resolved_tasks?.length === 1 && resolved_tasks[0]?.name === spec.task_names[0]
    && Boolean(config_hash && resolution_hash && resolved_at)
    && spec.concurrency === 1 && spec.attempts === 1 && !spec.agent_version && run.snapshot.agent.name === spec.agent
    && !Object.keys(spec.agent_kwargs ?? {}).some(key => /(^|[_-])version([_-]|$)/i.test(key.replace(/([a-z])([A-Z])/g, "$1_$2")))
    && Number.isInteger(spec.max_requests) && spec.max_requests >= 1 && spec.max_requests <= 10000
    && Number.isInteger(spec.max_output_tokens) && spec.max_output_tokens >= 1 && spec.max_output_tokens <= 1000000
    && Number.isInteger(spec.timeout_sec) && spec.timeout_sec >= 1 && spec.timeout_sec <= 86400
    && Number.isInteger(spec.setup_timeout_sec) && spec.setup_timeout_sec >= 1 && spec.setup_timeout_sec <= 86400
    && run.snapshot.provider.has_key && agentReasons(run.snapshot.agent, run.snapshot.model.protocol).length === 0;
}

export async function startConfirmedRun(api: Pick<APIClient, "startRun">, run: Run, input: StartInput): Promise<Run> {
  if (legacyDraft(run)) throw new Error("Recreate this historical draft with format-3 network profiles. Historical sources are not migrated automatically.");
  if (!boundedDraft(run) || input.confirm_paid !== true || input.config_hash !== run.snapshot.config_hash) throw new Error("Explicitly approve payment for this exact immutable draft and configuration hash before starting one task.");
  const risks = runTLSRisks(run);
  if (risks.execution && input.confirm_execution_tls_risk !== true) throw new Error("Independently approve the execution TLS risk for this draft.");
  if (risks.model && input.confirm_model_tls_risk !== true) throw new Error("Independently approve the model TLS risk for this draft.");
  return api.startRun(run.id, { confirm_paid: true, config_hash: run.snapshot.config_hash,
    ...(risks.execution ? { confirm_execution_tls_risk: true } : {}),
    ...(risks.model ? { confirm_model_tls_risk: true } : {}),
  });
}
