# OpenShift Argo CronWorkflow Dashboard

OpenShift içinde çalışan servis, Azure Repos’taki her proje JSON key’inden namespace üretir: key küçük harfe çevrilir ve `_` karakterleri `-` olur. Yalnızca `Type` değeri `CustomServe` olan ve `serving` listesinde harf duyarsız `bch` eşleşmesi bulunan projelerin namespace’leri izlenir. CronWorkflow `spec.workflowSpec` altındaki image’lardan yalnızca adı namespace/proje adını içerenler seçilir; image ID son `:` parçasından alınır ve `NEXUS_URL/bch-{namespace}/manifests/{imageId}` adresine GET atılır. HTTP 200 `exist`, 404 `missing`, diğer HTTP durumları veya bağlantı hataları `error` gösterir. Argo `CronWorkflow` ve `Workflow` kaynaklarıyla ilişkili pod’lar informer cache üzerinden izlenir.

## Geliştirme ve container oluşturma

Go 1.24 ve Node.js 22 gerekir.

```sh
cd web && npm ci && npm run build
cd ..
go build ./...
docker build -t cronworkflow-dashboard:latest .
```

## Dummy veriler ve testler

```sh
go test ./... -count=1
npm run build --prefix web
```

`internal/projects/testdata/projects.json`, örnek namespace/`serving` verisini; `internal/cluster/testdata/` altındaki JSON dosyaları CronWorkflow, iki Workflow ve pod’ları içerir. Testler BCH namespace seçimini, son run ve bağlı pod eşleşmesini, timezone’a göre sonraki schedule’ı ve boş image referansını kontrol eder. Registry testi yerel TLS fake registry kullanır; mevcut/missing sonuçlarını ve cache hit’inde tekrar istek gitmediğini doğrular. Testler gerçek Azure, OpenShift veya şirket registry’sine bağlanmaz.

Container, `:8080` portunda API ve arayüzü aynı origin üzerinden sunar. `/healthz` liveness ve readiness probe’ları için kullanılır.

## OpenShift kurulumu

`deploy/openshift.yaml` içindeki `cronworkflow-dashboard-config` ConfigMap’ini kurumunuzun değerleriyle güncelleyin. `AZURE_REPO_URL`, branch ve path proje JSON’unu almak içindir. `NEXUS_URL`, CronWorkflow image tag’inden çıkarılan ID’nin ekleneceği temel URL’dir. Diğer servis ayarları da ConfigMap’ten `envFrom` ile alınır. Namespace varsayılanı `workflow-monitoring`.

Deployment, amd64 ve arm64 platformlarını içeren Docker Hub’daki `mustafa12/monitor:0.0.18` image’ını kullanır. Uygulamanın kontrol ettiği Nexus endpoint’i ayrı `NEXUS_URL` ConfigMap ayarıdır; image dağıtım registry’siyle karıştırılmamalıdır.

```sh
oc apply -f deploy/openshift.yaml
```

Örnek `project.json` yapısı:

```json
{
  "payments-api": {
    "serving": ["BCH"],
    "team": "payments"
  },
  "internal-tool": {
    "serving": ["OTHER"],
    "team": "platform"
  }
}
```

JSON’un kök seviyesi proje key’lerinin bulunduğu bir object olmalıdır. Tüm key’ler namespace’e dönüştürülür. Image kontrolü için `Type` değeri `CustomServe`, `serving` alanı ise `bch` içermelidir; eşleştirmeler büyük/küçük harfe duyarsızdır. Namespace’ler proje refresh aralığında yenilenir. Azure kaynağı geçici olarak erişilemezse son başarılı namespace listesi kullanılmaya devam eder ve kaynak “degraded” görünür.

### Azure kimlik doğrulaması

Azure Repos anonim okumaya açık değilse PAT’i Secret’a ekleyin. Deployment Secret değerlerini `envFrom.secretRef` ile alır:

```sh
oc create secret generic cronworkflow-dashboard-secrets \
  --from-file=AZURE_TOKEN=./azure-token \
  -n workflow-monitoring
```

Azure DevOps erişimi gerekiyorsa ilgili token’a repo içeriğini okuma yetkisi verin. PAT’in Secret key’i `AZURE_TOKEN` olmalıdır. Pod’a Secret’tan ortam değişkeni olarak aktarılır; loglanmaz.

Nexus kontrolü anonim HTTP GET kullanır; bu sürümde Nexus username/password yoktur. CronWorkflow `spec.workflowSpec` altındaki image adı normalize namespace/proje adını içermiyorsa kontrol edilmez. Eşleşen image değerinin son `:` parçası image ID kabul edilir ve slash ile biten `NEXUS_URL` sonuna `bch-{namespace}/manifests/{imageId}` eklenir (örnek: `https://repomaster.company.com/repository/company-private/v2/mlops/bch-yazi-girisi-model/manifests/ald7383jdls8373`). Arayüzde image ID ve isteğin tam URL’si gösterilir. Her dashboard lookup’ı, başlatılan kontrol sayısını ve cache durumunu `INFO` seviyesinde pod loguna yazar; gerçek GET URL’si ile sonuç/hata da `INFO` seviyesinde loglanır. Yanıt 200 ise `exist`, 404 ise `missing`, diğer HTTP durumları veya istek hataları `error` durumudur. Sonuçlar varsayılan olarak 24 saat cache’lenir; `IMAGE_CACHE_TTL` ile değiştirilebilir. Aynı anda yapılan kontroller `REGISTRY_CHECK_CONCURRENCY` ile sınırlanır.

## Kubernetes ve Argo erişimi

Deployment cluster içinde ServiceAccount kullanır. ClusterRole yalnızca aşağıdaki kaynaklara `get`, `list`, `watch` izni verir:

- `argoproj.io/cronworkflows`
- `argoproj.io/workflows`
- core `pods`

ClusterRoleBinding bu okumayı cluster genelinde seçilen namespace’lere uygular; Kubernetes list/watch çağrıları yalnızca proje JSON’unda BCH olarak seçilen namespace’lere namespace-scoped yapılır. Argo Workflows CRD’lerinin `argoproj.io/v1alpha1` sürümü cluster’da kurulu olmalıdır. RBAC verilmeden önce manifestteki ClusterRoleBinding kapsamını platform ekibinizle doğrulayın.

Servis varsayılan olarak 5 dakikada Azure project JSON’u ve namespace values dosyalarını yeniler; Nexus image sonuçlarını 24 saat cache’ler. Arayüz API’yi 30 saniyede yeniler. Dashboard isteği cache süresi dolmuş image kontrollerini başlatır; günlük cache aynı image için tekrarlanan Nexus isteklerini sınırlar. Kubernetes okumaları her tarayıcı isteğinde tekrarlanmaz; informer cache’den sunulur.

## Ağ erişimi

Uygulama kullanıcı girişi sunmaz. Route’u yalnızca iç ağdan erişilebilir tutun veya kurumunuzun ingress, firewall ve kimlik doğrulama katmanlarıyla erişimi sınırlandırın. Pod’un Azure Repos ve gerekli container registry endpoint’lerine çıkış izni olmalıdır.

## Gösterilen durumlar

- Workflow listesi Azure’daki BCH namespace’leriyle sınırlıdır.
- Son retained Workflow, `workflows.argoproj.io/scheduled-time` annotation’ına göre seçilir; yoksa creation time kullanılır. Workflow history prune edilmişse son schedule bilgisi gösterilir, run status “Unavailable” kalır.
- Pod’lar `workflows.argoproj.io/workflow` etiketiyle Workflow’a bağlanır.
- Zamanlama hesabında CronWorkflow timezone’u kullanılır. Timezone alanı yoksa next run UTC varsayımıyla hesaplanır ve uyarı gösterilir.
- Namespace, Workflow status ve image status filtreleri birlikte kullanılabilir.
- Azure, OpenShift ve container registry kaynak durumları ile en son başarılı güncellenme zamanı sayfanın üstünde gösterilir. Image HTTP/bağlantı hataları image satırında `error` görünür ve registry kaynağını `degraded` yapar; geçersiz veya boş image referansı `unknown` görünür.
