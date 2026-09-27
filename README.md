# OpenShift Argo CronWorkflow Dashboard

OpenShift içinde çalışan bu servis, Azure Repos’taki `project.json` dosyasından `serving` listesinde `BCH` olan namespace’leri seçer. Argo `CronWorkflow` ve `Workflow` kaynaklarını, bu workflow’lara bağlı pod’ları informer cache üzerinden izler. React arayüzü son çalışmayı, pod durumlarını, cron zamanını ve container image’larının registry’de bulunup bulunmadığını gösterir.

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

`deploy/openshift.yaml` içindeki Azure URL, branch ve dosya yolunu düzenleyin. Namespace varsayılanı `workflow-monitoring`; kurumunuzun namespace’ine göre dosyanın tamamında aynı değeri kullanın. Image satırını kullandığınız registry ve tag’e göre değiştirin.

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

JSON’un kök seviyesi namespace adlarının key olduğu bir object olmalıdır. `serving` alanı string array’idir; `BCH` karşılaştırması büyük/küçük harfe duyarsız yapılır. Namespace’ler proje refresh aralığında yenilenir. Azure kaynağı geçici olarak erişilemezse son başarılı namespace listesi kullanılmaya devam eder ve kaynak “degraded” görünür.

### Azure kimlik doğrulaması

Repo anonim okumaya açık değilse PAT dosyasını Secret olarak ekleyip deployment’a dosya yolunu bildirin:

```sh
oc create secret generic cronworkflow-azure-token \
  --from-file=token=./azure-token \
  -n workflow-monitoring
oc set env deployment/cronworkflow-dashboard \
  AZURE_TOKEN_FILE=/var/run/azure/token \
  -n workflow-monitoring
```

PAT yalnızca pod içindeki salt okunur Secret mount’unda tutulur. Azure DevOps erişimi gerekiyorsa ilgili token’a repo içeriğini okuma yetkisi verin.

### Private registry erişimi

Image kontrolü için registry kullanıcı bilgilerini Docker `config.json` olarak Secret’a koyun:

```sh
oc create secret generic cronworkflow-registry-auth \
  --from-file=config.json=./config.json \
  -n workflow-monitoring
oc set env deployment/cronworkflow-dashboard \
  REGISTRY_AUTH_FILE=/var/run/registry/config.json \
  -n workflow-monitoring
```

Secret mount edildiğinde servis `DOCKER_CONFIG` değerini bu dosyanın dizinine ayarlar ve go-containerregistry varsayılan keychain’ini kullanır. Public registry’ler için bu Secret gerekli değildir. Manifest isteğinin başarılı olması `present`, HTTP 404 `missing` sonucunu verir. Yetki, ağ, timeout ve diğer hatalar `unknown` olarak gösterilir. Varsayılan image cache süresi 5 dakika, aynı anda yapılan registry kontrolü sayısı 4’tür.

## Kubernetes ve Argo erişimi

Deployment cluster içinde ServiceAccount kullanır. ClusterRole yalnızca aşağıdaki kaynaklara `get`, `list`, `watch` izni verir:

- `argoproj.io/cronworkflows`
- `argoproj.io/workflows`
- core `pods`

ClusterRoleBinding bu okumayı cluster genelinde seçilen namespace’lere uygular; Kubernetes list/watch çağrıları yalnızca proje JSON’unda BCH olarak seçilen namespace’lere namespace-scoped yapılır. Argo Workflows CRD’lerinin `argoproj.io/v1alpha1` sürümü cluster’da kurulu olmalıdır. RBAC verilmeden önce manifestteki ClusterRoleBinding kapsamını platform ekibinizle doğrulayın.

Servis varsayılan olarak 5 dakikada Azure project JSON’u yeniler ve image sonuçlarını 5 dakika cache’ler. Değerler `PROJECT_REFRESH_INTERVAL`, `IMAGE_CACHE_TTL` ve `REGISTRY_CHECK_CONCURRENCY` environment değişkenleriyle değiştirilebilir. Arayüz API’yi 30 saniyede yeniler. Kubernetes okumaları her tarayıcı isteğinde tekrarlanmaz; informer cache’den sunulur.

## Ağ erişimi

Uygulama kullanıcı girişi sunmaz. Route’u yalnızca iç ağdan erişilebilir tutun veya kurumunuzun ingress, firewall ve kimlik doğrulama katmanlarıyla erişimi sınırlandırın. Pod’un Azure Repos ve gerekli container registry endpoint’lerine çıkış izni olmalıdır.

## Gösterilen durumlar

- Workflow listesi Azure’daki BCH namespace’leriyle sınırlıdır.
- Son retained Workflow, `workflows.argoproj.io/scheduled-time` annotation’ına göre seçilir; yoksa creation time kullanılır. Workflow history prune edilmişse son schedule bilgisi gösterilir, run status “Unavailable” kalır.
- Pod’lar `workflows.argoproj.io/workflow` etiketiyle Workflow’a bağlanır.
- Zamanlama hesabında CronWorkflow timezone’u kullanılır. Timezone alanı yoksa next run UTC varsayımıyla hesaplanır ve uyarı gösterilir.
- Namespace, Workflow status ve image status filtreleri birlikte kullanılabilir.
- Azure, OpenShift ve container registry kaynak durumları ile en son başarılı güncellenme zamanı sayfanın üstünde gösterilir. Hatalı veya boş image referansı image satırında `unknown` görünür; registry kaynağını tek başına `degraded` yapmaz.
