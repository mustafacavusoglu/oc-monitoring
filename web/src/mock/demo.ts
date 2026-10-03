/**
 * Dummy /api/dashboard payload for `npm run dev`. Only the Vite dev server
 * imports this file, so it never reaches the production bundle. Output is
 * deterministic (seeded) and timestamps are relative to `now`.
 */
import type { CronWorkflow, DashboardResponse, Model, Pod, Project, RunSummary } from '../types.ts'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

function seeded(seed: number) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

type ModelSeed = [namespace: string, name: string, runtime: string, format: string, gpu: number, replicas: [number, number], state?: Model['state'], reason?: string, message?: string]

/** Models running on MIG slices instead of full GPUs (per replica). */
const MIG_BY_MODEL: Record<string, Record<string, number>> = {
  'phi-3-mini-dev': { '3g.20gb': 1 },
  'bge-m3-embedding': { '2g.10gb': 1 },
  'fraud-xgboost': { '1g.5gb': 1 },
  'ocr-detector': { '1g.5gb': 1 },
  'ocr-recognizer': { '1g.5gb': 2 },
  'wav2vec2-tr': { '2g.10gb': 1 },
}

const LLM_IMAGE = 'quay.io/modh/vllm:rhoai-2.19-cuda'
const TRITON_IMAGE = 'nvcr.io/nvidia/tritonserver:24.08-py3'

const LLM_SEEDS: ModelSeed[] = [
  ['chatbot-asistan', 'llama-3-1-8b-chat', 'vllm-runtime', 'vLLM', 1, [1, 3]],
  ['chatbot-asistan', 'qwen2-5-14b-instruct', 'vllm-runtime', 'vLLM', 2, [1, 2]],
  ['belge-ozet', 'mistral-7b-ozet', 'vllm-runtime', 'vLLM', 1, [1, 2]],
  ['belge-ozet', 'granite-3-1-8b-ozet', 'vllm-runtime', 'vLLM', 1, [1, 1], 'NotReady', 'RevisionFailed', 'Container kserve-container: CUDA out of memory (OOMKilled)'],
  ['rag-platform', 'llama-3-1-70b-instruct', 'vllm-multinode', 'vLLM', 4, [1, 1]],
  ['rag-platform', 'bge-m3-embedding', 'vllm-runtime', 'vLLM', 1, [1, 4]],
  ['yazi-girisi-model', 'yazi-duzeltme-llm', 'vllm-runtime', 'vLLM', 1, [0, 2]],
  ['mlops-development', 'llm-smoke-test', 'vllm-runtime', 'vLLM', 1, [1, 1], 'Unknown'],
  ['mlops-development', 'phi-3-mini-dev', 'vllm-runtime', 'vLLM', 1, [1, 1]],
  ['ses-tanima', 'whisper-large-v3', 'vllm-runtime', 'vLLM', 1, [1, 2]],
]

const LLMISVC_SEEDS: ModelSeed[] = [
  ['rag-platform', 'granite-rag-llmd', 'LLMInferenceService', 'granite-3.1-8b-instruct', 1, [2, 2]],
  ['chatbot-asistan', 'llama-guard-3', 'LLMInferenceService', 'llama-guard-3-8b', 1, [1, 1], 'NotReady', 'MinimumReplicasUnavailable', 'Deployment does not have minimum availability: 0/1 pods ready'],
]

const ML_SEEDS: ModelSeed[] = [
  ['fraud-detection', 'fraud-xgboost', 'triton-runtime', 'onnx', 0, [2, 6]],
  ['fraud-detection', 'fraud-graph-gnn', 'triton-runtime', 'pytorch', 1, [1, 2]],
  ['kredi-skor', 'kredi-skor-lgbm', 'triton-runtime', 'onnx', 0, [2, 4]],
  ['kredi-skor', 'limit-onerici', 'triton-runtime', 'onnx', 0, [1, 2], 'NotReady', 'ModelLoadFailed', 'failed to load model limit-onerici: version 3 not found in model repository'],
  ['churn-tahmin', 'churn-catboost', 'triton-runtime', 'onnx', 0, [1, 3]],
  ['musteri-segment', 'segment-kmeans', 'triton-runtime', 'python', 0, [1, 1]],
  ['ocr-servis', 'ocr-detector', 'triton-runtime', 'tensorrt', 1, [1, 3]],
  ['ocr-servis', 'ocr-recognizer', 'triton-runtime', 'tensorrt', 1, [1, 3]],
  ['yazi-girisi-model', 'yazi-siniflandirici', 'triton-runtime', 'onnx', 0, [1, 2]],
  ['fiyat-optimizasyon', 'fiyat-elastikiyet', 'triton-runtime', 'python', 0, [1, 2]],
  ['ses-tanima', 'wav2vec2-tr', 'triton-runtime', 'pytorch', 1, [1, 2]],
  ['mlops-development', 'triton-ensemble-dev', 'triton-runtime', 'ensemble', 0, [1, 1], 'Unknown'],
  ['musteri-segment', 'oneri-motoru', 'triton-runtime', 'tensorflow', 0, [1, 2]],
  ['churn-tahmin', 'churn-explainer', 'triton-runtime', 'python', 0, [1, 1], 'NotReady', 'RevisionMissing', 'Revision "churn-explainer-predictor-00004" failed with message: ImagePullBackOff'],
]

const BATCH_SEEDS: [namespace: string, name: string, schedule: string][] = [
  ['yazi-girisi-model', 'gunluk-skorlama', '0 2 * * *'],
  ['yazi-girisi-model', 'model-yeniden-egitim', '0 4 * * 1'],
  ['kredi-skor', 'kredi-skor-batch', '30 1 * * *'],
  ['kredi-skor', 'erken-uyari-raporu', '0 */6 * * *'],
  ['fraud-detection', 'fraud-gece-tarama', '15 0 * * *'],
  ['fraud-detection', 'feature-guncelleme', '*/30 * * * *'],
  ['churn-tahmin', 'churn-haftalik', '0 5 * * 1'],
  ['churn-tahmin', 'churn-gunluk-skor', '0 3 * * *'],
  ['musteri-segment', 'segment-yenileme', '0 6 * * *'],
  ['musteri-segment', 'oneri-precompute', '0 */4 * * *'],
  ['fiyat-optimizasyon', 'fiyat-hesaplama', '0 7 * * *'],
  ['fiyat-optimizasyon', 'rakip-fiyat-cekme', '0 */2 * * *'],
  ['belge-ozet', 'toplu-ozetleme', '0 22 * * *'],
  ['ocr-servis', 'arsiv-ocr', '0 23 * * 0-4'],
  ['rag-platform', 'vektor-indeksleme', '0 1 * * *'],
  ['rag-platform', 'dokuman-senkron', '*/15 * * * *'],
  ['ses-tanima', 'cagri-transkript', '0 */3 * * *'],
  ['mlops-development', 'drift-izleme', '0 8 * * *'],
  ['mlops-development', 'veri-kalite-kontrol', '0 9 * * 1-5'],
  ['chatbot-asistan', 'konusma-analizi', '30 23 * * *'],
]

/** Next fire time for the simple cron forms used above (minute/hour fields only). */
function nextRun(schedule: string, now: number): number {
  const [minute, hour] = schedule.split(' ')
  const matches = (field: string, value: number) =>
    field === '*' || (field.startsWith('*/') ? value % Number(field.slice(2)) === 0 : Number(field) === value)
  const start = Math.ceil(now / MINUTE) * MINUTE
  for (let t = start; t < start + 8 * DAY; t += MINUTE) {
    const date = new Date(t)
    if (matches(minute, date.getMinutes()) && matches(hour, date.getHours())) return t
  }
  return start + DAY
}

const iso = (ms: number) => new Date(ms).toISOString()

function buildModel([namespace, name, runtime, format, gpu, [min, max], state = 'Ready', reason, message]: ModelSeed, type: Model['type'], index: number, now: number): Model {
  const isLLMISVC = runtime === 'LLMInferenceService'
  const created = now - (index * 3 + 2) * DAY - index * HOUR
  return {
    namespace, name, type,
    kind: isLLMISVC ? 'LLMInferenceService' : 'InferenceService',
    runtime: isLLMISVC ? undefined : runtime,
    image: isLLMISVC ? 'ghcr.io/llm-d/llm-d:0.2.0' : type === 'llm' ? LLM_IMAGE : TRITON_IMAGE,
    modelFormat: format,
    storageUri: isLLMISVC ? `hf://${format}` : `s3://mlops-models/${namespace}/${name}/v${(index % 4) + 1}`,
    url: `https://${name}-${namespace}.apps.ocp.example.local`,
    state,
    reason: state === 'Ready' ? undefined : reason,
    message,
    minReplicas: min,
    maxReplicas: max,
    gpu: MIG_BY_MODEL[name] ? 0 : gpu,
    mig: MIG_BY_MODEL[name],
    createdAt: iso(created),
    stateSince: iso(state === 'Ready' ? created + 6 * MINUTE : now - (index + 1) * 47 * MINUTE),
  }
}

function buildHistory(random: () => number, count: number, start: number, intervalMs: number, failRate: number): RunSummary[] {
  return Array.from({ length: count }, (_, i) => {
    const startedAt = start - i * intervalMs
    const failed = random() < failRate
    return {
      name: `run-${Math.floor(startedAt / MINUTE).toString(36)}`,
      phase: failed ? (random() < 0.3 ? 'Error' : 'Failed') : 'Succeeded',
      startedAt: iso(startedAt),
      finishedAt: iso(startedAt + (4 + random() * 40) * MINUTE),
    }
  })
}

function buildCronWorkflow([namespace, name, schedule]: (typeof BATCH_SEEDS)[number], index: number, random: () => number, now: number): CronWorkflow {
  const suspended = index === 13
  const running = index % 6 === 1
  const failing = index === 2 || index === 9 || index === 16
  const missingImage = index === 4 || index === 11
  const neverRan = index === 18
  const interval = schedule.startsWith('*/') ? Number(schedule.split(' ')[0].slice(2)) * MINUTE
    : schedule.split(' ')[1].startsWith('*/') ? Number(schedule.split(' ')[1].slice(2)) * HOUR : DAY

  const history = neverRan ? [] : buildHistory(random, 6 + (index % 5), now - interval * (running ? 0.02 : 0.4), interval, 0.12)
  if (history[0]) history[0] = { ...history[0], phase: failing ? 'Failed' : 'Succeeded' }
  if (running && history[0]) history[0] = { ...history[0], phase: 'Running', finishedAt: undefined }
  const latest = history[0]
  const imageId = `${(index * 7919).toString(16)}a${(index * 104729).toString(16)}`

  const podPhase = latest?.phase === 'Running' ? 'Running' : latest?.phase === 'Succeeded' ? 'Succeeded' : 'Failed'
  return {
    namespace, name, project: namespace, schedules: [schedule],
    timezone: index % 5 === 0 ? 'UTC (assumed)' : 'Europe/Istanbul',
    suspended,
    active: running,
    lastScheduledAt: latest?.startedAt,
    nextScheduledAt: suspended ? undefined : iso(nextRun(schedule, now)),
    scheduleError: index % 5 === 0 ? 'timezone unset; next run assumes UTC' : undefined,
    history,
    lastRun: latest && {
      ...latest,
      scheduledAt: latest.startedAt,
      createdAt: latest.startedAt,
      pods: [
        { name: `${latest.name}-hazirlik`, phase: 'Succeeded', ready: 0, containers: 1, restarts: 0, containerStates: ['main: Terminated (Completed)'] },
        {
          name: `${latest.name}-skorlama`, phase: podPhase, ready: podPhase === 'Running' ? 1 : 0, containers: 1, restarts: 0,
          containerStates: [podPhase === 'Running' ? 'main: Running' : podPhase === 'Failed' ? 'main: Terminated (Error)' : 'main: Terminated (Completed)'],
        },
      ],
    },
    images: [{
      reference: `repomaster.company.com/mlops/bch-${namespace}/${name}:${imageId}`,
      imageId,
      url: `https://repomaster.company.com/repository/company-private/v2/mlops/bch-${namespace}/manifests/${imageId}`,
      status: missingImage ? 'missing' : 'exist',
      checkedAt: iso(now - (index + 1) * 13 * MINUTE),
    }],
  }
}

const CUSTOM_SEEDS: ModelSeed[] = [
  ['kampanya-oneri', 'kampanya-skor-api', 'custom-fastapi-runtime', 'sklearn', 0, [2, 4]],
  ['kampanya-oneri', 'segment-api', 'custom-fastapi-runtime', 'sklearn', 0, [1, 2], 'NotReady', 'MinimumReplicasUnavailable', 'Deployment does not have minimum availability'],
  ['adres-eslestirme', 'adres-normalize', 'custom-torchserve', 'pytorch', 1, [1, 2]],
  ['dolandiricilik-kural', 'kural-motoru', 'custom-java-runtime', 'pmml', 0, [2, 2]],
]

/** Projects not represented by any resource above: one without a namespace, one empty. */
const EMPTY_PROJECTS: Project[] = [
  { key: 'ESKI_KAMPANYA', namespace: 'eski-kampanya', batch: true, customServe: false, namespaceExists: false, cronWorkflows: 0, inferenceServices: 0, pods: 0 },
  { key: 'YENI_SKOR_MODEL', namespace: 'yeni-skor-model', batch: true, customServe: false, namespaceExists: true, cronWorkflows: 0, inferenceServices: 0, pods: 0 },
  { key: 'BELGE_SINIFLANDIRMA', namespace: 'belge-siniflandirma', batch: false, customServe: true, namespaceExists: true, cronWorkflows: 0, inferenceServices: 0, pods: 0 },
]

function customPods(model: Model, index: number, now: number): Pod[] {
  return Array.from({ length: model.minReplicas || 1 }, (_, replica) => {
    const crashing = model.state === 'NotReady' && replica === 0
    return {
      name: `${model.name}-predictor-${(index * 7 + replica).toString(36)}x${replica}`,
      phase: 'Running',
      ready: crashing ? 1 : 2,
      containers: 2,
      restarts: crashing ? 14 : replica,
      node: `worker-${(index + replica) % 6 + 1}`,
      startedAt: iso(now - (index + 1) * 5 * HOUR),
      containerStates: crashing
        ? ['kserve-container: Waiting (CrashLoopBackOff)', 'queue-proxy: Running']
        : ['kserve-container: Running', 'queue-proxy: Running'],
    }
  })
}

export function createDemoDashboard(date: Date): DashboardResponse {
  const now = date.getTime()
  const random = seeded(42)
  const models = [
    ...LLM_SEEDS.map((seed, i) => buildModel(seed, 'llm', i, now)),
    ...LLMISVC_SEEDS.map((seed, i) => buildModel(seed, 'llm', i + LLM_SEEDS.length, now)),
    ...ML_SEEDS.map((seed, i) => buildModel(seed, 'ml', i, now)),
    ...CUSTOM_SEEDS.map((seed, i) => {
      const model = { ...buildModel(seed, 'custom', i, now), image: `repomaster.company.com/mlops/${seed[2]}:1.${i}` }
      return { ...model, pods: customPods(model, i, now) }
    }),
  ].sort((a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name))
  const cronWorkflows = BATCH_SEEDS.map((seed, i) => buildCronWorkflow(seed, i, random, now))
  const ok = { state: 'ready', lastSuccess: iso(now - 40_000) }
  const projects: Project[] = [
    ...[...new Set(cronWorkflows.map((w) => w.namespace))].map((namespace) => ({
      key: namespace.toUpperCase().replaceAll('-', '_'), namespace, batch: true, customServe: false, namespaceExists: true,
      cronWorkflows: cronWorkflows.filter((w) => w.namespace === namespace).length, inferenceServices: 0, pods: 2,
    })),
    ...[...new Set(models.filter((m) => m.type === 'custom').map((m) => m.namespace))].map((namespace) => {
      const own = models.filter((m) => m.namespace === namespace)
      return {
        key: namespace.toUpperCase().replaceAll('-', '_'), namespace, type: 'CustomServe', batch: false, customServe: true, namespaceExists: true,
        cronWorkflows: 0, inferenceServices: own.length, pods: own.reduce((sum, m) => sum + (m.pods?.length ?? 0), 0),
      }
    }),
    ...EMPTY_PROJECTS,
  ].sort((a, b) => a.key.localeCompare(b.key))

  return {
    generatedAt: iso(now),
    refreshIntervalSeconds: 30,
    sources: { projects: ok, cluster: ok, pods: ok, registry: { ...ok, lastSuccess: iso(now - 13 * MINUTE) } },
    namespaces: [...new Set([...models, ...cronWorkflows, ...projects].map((item) => item.namespace))].sort(),
    projects,
    models,
    cronWorkflows,
  }
}
