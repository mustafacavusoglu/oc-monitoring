# MLOps Monitor

OpenShift üzerinde çalışan LLM modellerini, ML modellerini ve batch (Argo CronWorkflow) işlerini tek ekranda izleyen ekip dashboard’u. Sol menüden tür seçilir, üst bardan namespace filtrelenir (varsayılan: tüm namespace’ler). Seçili sayfa ve namespace URL’de tutulur (`#/batch?ns=kredi-skor`), bu yüzden filtreli görünüm link olarak paylaşılabilir.

| Sayfa | Kaynak | Sınıflandırma |
|---|---|---|
| LLM modelleri | `LLMInferenceService`, `InferenceService` | Her `LLMInferenceService` LLM’dir. `InferenceService`, `spec.predictor.model.runtime` ile bağlı olduğu `ServingRuntime` image’ı `LLM_RUNTIME_IMAGE_KEYWORDS` (ör. `vllm`) içeriyorsa LLM’dir. |
| ML modelleri | `InferenceService` | Runtime image’ı `ML_RUNTIME_IMAGE_KEYWORDS` (ör. `triton`) içeriyorsa ML’dir. İki listeye de uyan runtime LLM sayılır; hiçbirine uymayanlar gösterilmez. |
| Custom Serve | `InferenceService`, `ServingRuntime`, pod | Runtime image’ı LLM veya ML anahtar kelimelerine uymayan (ya da runtime’ı bulunamayan) tüm `InferenceService`’ler. |
| Batch modelleri | `CronWorkflow`, `Workflow`, pod | Cluster’daki **tüm** namespace’lerin CronWorkflow’ları, son Workflow’ları ve pod’ları. |

InferenceService, `spec.predictor.model.runtime` alanındaki adla aynı namespace’teki ServingRuntime’a bağlanır: türü runtime image’ı belirler, InferenceService GPU/MIG tanımlamıyorsa runtime container’larının kaynakları sayılır, adı verilen ServingRuntime yoksa model sorunlu görünür. Her şey OpenShift’ten okunur ve türler kaynaklardan çıkarılır; proje JSON’u yalnızca proje namespace’lerini verir. Her `InferenceService`’in pod’ları (`serving.kserve.io/inferenceservice` etiketi) hazır/restart bilgisiyle model satırında gösterilir.

**Projeler** sayfası proje JSON’undaki her projeyi cluster’da bulunanlarla yan yana gösterir: namespace var mı, kaç CronWorkflow, InferenceService ve pod bulundu. Kaynağı eksik projeler en üstte listelenir; böylece dashboard’da görünmeyen bir projenin nedeni (namespace yok, kaynak yok) açıkça görülür. Okunamayan proje kayıtları (ör. JSON object olmayan değer veya aynı namespace’e dönüşen iki anahtar) tüm listeyi bozmaz; atlanır ve kaynak durumunda listelenir.

**Filtreleme:** Her grafikteki çubuk, segment, donut dilimi ve KPI kartı tıklanınca o sayfanın listesini filtreler; Genel bakış’taki grafikler ilgili türün sayfasını o filtreyle açar (ör. Sağlık durumu → Batch · Sorunlu). Aktif filtreler listenin üstünde kaldırılabilir etiketler olarak görünür ve URL’de tutulur (`#/batch?ns=kredi-skor&f=critical`), bu yüzden filtreli görünüm link olarak paylaşılabilir.

Genel bakış sayfası tüm türlerin sayılarını, namespace dağılımını, sağlık durumunu ve “dikkat gerektirenler” listesini (hazır olmayan modeller, son çalışması başarısız olan veya image’ı Nexus’ta bulunmayan batch işleri) gösterir.

## Performans

- Kubernetes verisi informer cache’inden okunur; tarayıcı isteği hiçbir zaman Kubernetes, Azure veya Nexus’u beklemez.
- CronWorkflow, Workflow, InferenceService, ServingRuntime, LLMInferenceService ve Namespace kaynakları cluster genelinde kaynak başına tek watch ile izlenir ve projelere bellekte filtrelenir. Yalnızca pod’lar proje namespace’lerinde ayrı ayrı izlenir.
- Cache’e alınan nesnelerden `managedFields` ve `last-applied-configuration` atılır; nesneler kopyalanmadan okunur.
- Nexus kontrolleri arka planda yapılır, sonuçlar `IMAGE_CACHE_TTL` boyunca cache’lenir ve eşzamanlılık `REGISTRY_CHECK_CONCURRENCY` ile sınırlanır.
- API yanıtı gzip ile, hash’li JS/CSS dosyaları build sırasında önceden sıkıştırılmış olarak ve `immutable` cache başlığıyla sunulur.
- Arayüz `UI_REFRESH_INTERVAL` aralığıyla yenilenir, sekme gizliyken yenilemeyi durdurur. Grafikler bağımlılıksız SVG/CSS bileşenleridir.

## Konfigürasyon

Ortama özgü bütün değerler `deploy/openshift.yaml` içindeki `mlops-dashboard-config` ConfigMap’inden `envFrom` ile gelir. Kodda varsayılan değer yoktur; eksik veya hatalı anahtarlar açılışta tek bir hata mesajında listelenir ve servis başlamaz.

| Anahtar | Açıklama |
|---|---|
| `HTTP_ADDR`, `WEB_DIR` | Dinlenen adres ve arayüz dosyalarının dizini (`/app/web`). |
| `OPENSHIFT_CONSOLE_URL` | OpenShift web console adresi; kaynak adlarının yanındaki ↗ linkleri burada yeni sekmede açılır (`/k8s/ns/<namespace>/<group~version~Kind>/<ad>`). |
| `UI_REFRESH_INTERVAL` | Arayüzün yenileme aralığı (ör. `30s`). |
| `UPSTREAM_TIMEOUT` | Azure ve Nexus HTTP istek zaman aşımı. |
| `AZURE_REPO_URL`, `AZURE_REPO_BRANCH`, `AZURE_PROJECTS_PATH` | Proje JSON’unun Azure Repos konumu. |
| `PROJECT_REFRESH_INTERVAL` | Proje JSON’unun yenilenme aralığı. |
| `NEXUS_MANIFEST_URL_TEMPLATE` | `{namespace}` ve `{imageId}` içeren manifest URL şablonu. |
| `IMAGE_CACHE_TTL`, `REGISTRY_CHECK_CONCURRENCY` | Nexus sonuç cache süresi ve aynı anda yapılabilecek kontrol sayısı. |
| `LLM_RUNTIME_IMAGE_KEYWORDS`, `ML_RUNTIME_IMAGE_KEYWORDS` | Virgülle ayrılmış ServingRuntime image anahtar kelimeleri. |
| `GPU_RESOURCE_NAME` | GPU kaynak adı (ör. `nvidia.com/gpu`). |
| `MIG_RESOURCE_PREFIX` | MIG dilimi kaynak öneki (ör. `nvidia.com/mig-`); kalan kısım profil adıdır (`1g.5gb`, `3g.20gb`). |
| `CRONWORKFLOW_RESOURCE`, `WORKFLOW_RESOURCE`, `INFERENCE_SERVICE_RESOURCE`, `SERVING_RUNTIME_RESOURCE`, `LLM_INFERENCE_SERVICE_RESOURCE` | İzlenen API’ler, `group/version/resource` biçiminde. Cluster’daki CRD sürümü farklıysa buradan değiştirilir. |

Azure PAT, manifestteki `mlops-dashboard-secrets` Secret’ının `AZURE_TOKEN` alanından okunur ve loglanmaz. `oc apply` öncesinde `<AZURE_PAT>` yerine token’ı yerel kopyanızda yazın; gerçek token’ı commit etmeyin. Manifesti her uyguladığınızda Secret dosyadaki değerle güncellenir. Token’ı dosyaya yazmak istemezseniz Secret bloğunu silip Secret’ı komutla oluşturabilirsiniz:

```sh
oc create secret generic mlops-dashboard-secrets --from-literal=AZURE_TOKEN=<AZURE_PAT> -n mlops-development
```

### Proje JSON’u

```json
{
  "PAYMENTS_API": { "Serving": ["BCH"] },
  "YAZI_GIRISI_MODEL": { "Serving": ["BCH"] }
}
```

Yalnızca proje key’leri kullanılır: key küçük harfe çevrilir ve `_` karakterleri `-` olur; sonuç namespace adıdır (`PAYMENTS_API` → `payments-api`). Değerlerin içeriği okunmaz. Azure geçici olarak erişilemezse son başarılı liste kullanılmaya devam eder ve kaynak “Sorunlu” görünür.

### Nexus image kontrolü

CronWorkflow spec’indeki her `image:` alanı okunur (container, script, sidecar, init, containerSet veya inline template). Adında CronWorkflow’un namespace’i geçen image kontrol edilir; image ID son `:` sonrasıdır (digest için `@` sonrası). Şablondaki `{namespace}` CronWorkflow’un namespace’i, `{imageId}` bu ID’dir: `repomaster.../mlops/bch-yazi-girisi-model:ald7383jdls8373` → `.../bch-yazi-girisi-model/manifests/ald7383jdls8373`. Kontrol edilecek image bulunamayan CronWorkflow’lar Batch sayfasında “Image kontrolü yok” kartıyla listelenir. HTTP 200 `Mevcut`, 404 `Bulunamadı`, diğer durumlar `Hata` olarak gösterilir. Her istek, sonuç ve cache kullanımı `INFO` seviyesinde loglanır.

## Geliştirme

Go 1.24 ve Node.js 22 gerekir.

```sh
# Arayüzü dummy verilerle çalıştırma (backend gerekmez)
cd web && npm ci && npm run dev    # http://localhost:5173

# Testler ve build
go test ./... -count=1
npm run build --prefix web
docker buildx build --platform linux/amd64 -t mustafa12/monitor:0.0.30 --push .
```

`npm run dev`, Vite dev sunucusunda `/api/dashboard` isteğini `web/src/mock/demo.ts` içindeki deterministik dummy veriyle yanıtlar. Bu dosya yalnızca dev sunucusunda yüklenir, production bundle’a girmez.

Kod yapısı:

| Paket | Görev |
|---|---|
| `internal/config` | ConfigMap’ten gelen ortam değişkenlerini okur ve doğrular. |
| `internal/cluster` | Değişen namespace kümesi için genel informer watcher’ı. |
| `internal/projects` | Azure Repos’tan proje JSON’unu okuyup batch namespace’lerini seçer. |
| `internal/batch` | CronWorkflow satırlarını, çalışma geçmişini ve image kontrollerini üretir. |
| `internal/serving` | InferenceService/LLMInferenceService’leri LLM/ML/Custom Serve olarak sınıflandırır. |
| `internal/registry` | Nexus manifest kontrolü ve TTL cache. |
| `internal/dashboard` | Tek JSON uç noktası: `GET /api/dashboard`. |
| `web/src` | React arayüzü: `pages/` (sayfalar), `components/` (tablo, KPI, grafikler), `lib/` (durum, format, polling, routing). |

## OpenShift kurulumu

Manifest, mevcut `mlops-development` projesine kurulur:

```sh
oc apply -f deploy/openshift.yaml
```

ConfigMap değerlerini kurumunuza göre güncelleyin. Deployment `/healthz` ile probe edilir, `:8080` portunda API ve arayüzü aynı origin’den sunar ve Route üzerinden TLS edge ile yayınlanır.

### RBAC

ServiceAccount’a ClusterRole ile yalnızca `get`, `list`, `watch` izni verilir:

- `argoproj.io`: `cronworkflows`, `workflows`
- core: `pods`, `namespaces`
- `serving.kserve.io`: `inferenceservices`, `servingruntimes`, `llminferenceservices`

Kaynaklar cluster genelinde izlendiği için ClusterRoleBinding gerekir; pod okumaları yalnızca proje namespace’leriyle sınırlıdır. Kapsamı platform ekibinizle doğrulayın. Açılışta her API discovery ile kontrol edilir: cluster’da sunulmayan bir kaynak (ör. `LLMInferenceService` CRD’si kurulu değilse ya da sürümü farklıysa) atlanır, pod loguna `skipping resource not served by the cluster` yazılır ve kaynak durumunun açıklamasında gösterilir. CRD sonradan kurulursa pod’u yeniden başlatın.

### Ağ erişimi

Uygulama kullanıcı girişi sunmaz. Route’u yalnızca iç ağdan erişilebilir tutun veya kurumunuzun ingress, firewall ve kimlik doğrulama katmanlarıyla erişimi sınırlandırın. Pod’un Kubernetes API’sine, Azure Repos’a ve Nexus’a HTTPS ile erişebilmesi gerekir.

## Durum kuralları

- Model durumu kaynağın `Ready` condition’ından gelir: `True` → Hazır, `False` → Hazır değil (reason/message gösterilir), condition yoksa Bilinmiyor.
- Ayrılan GPU = replika başına GPU limiti (yoksa request) × `minReplicas` (belirtilmemişse 1). MIG dilimleri aynı şekilde profil bazında sayılır ve tam GPU’dan ayrı gösterilir; MIG “single” stratejisinde dilimler `nvidia.com/gpu` olarak göründüğü için tam GPU gibi sayılır.
- CronWorkflow’un son çalışması `workflows.argoproj.io/scheduled-time` annotation’ına, yoksa creation time’a göre seçilir. Çalışma geçmişi Argo’nun sakladığı Workflow’lardan oluşur (en fazla son 10 gösterilir).
- Pod’lar `workflows.argoproj.io/workflow` etiketiyle Workflow’a bağlanır.
- Sonraki çalışma CronWorkflow timezone’uyla hesaplanır; timezone yoksa UTC varsayılır ve uyarı gösterilir.
- Kaynak durumları (model, batch, proje listesi, Nexus) ve son başarılı güncellenme zamanları sol menünün altında gösterilir.
