# Kubernetes Deployment

In case you have deployed your `jellyfin` in a kubernetes environment, the exporter can be installed as either a
standalone deployment, or an extra container in your `jellyfin` deployment. This documentation provides an example of
standalone exporter, using `serviceMonitor` from the `kube-prometheus-stack`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jellyfin-exporter
  namespace: arr
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jellyfin-exporter
  template:
    metadata:
      labels:
        app: jellyfin-exporter
    spec:
      containers:
        - name: jellyfin-exporter
          image: rebelcore/jellyfin-exporter:latest
          args:
            - "--jellyfin.address=http://jellyfin:8096"
            - "--jellyfin.token-file=/etc/jellyfin-exporter/token"
            - "--collector.activity"
          volumeMounts:
            - name: jellyfin-api-key
              mountPath: /etc/jellyfin-exporter
              readOnly: true
          ports:
            - containerPort: 9594
              name: metrics
      volumes:
        - name: jellyfin-api-key
          secret:
            secretName: jellyfin-api-key
---
apiVersion: v1
kind: Service
metadata:
  name: jellyfin-exporter
  namespace: arr
  labels:
    app: jellyfin-exporter
spec:
  selector:
    app: jellyfin-exporter
  ports:
    - protocol: TCP
      port: 9594
      targetPort: 9594
      name: metrics
  type: ClusterIP
---
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: jellyfin-api-key
  namespace: arr
data:
  token: <your-admin-token-in-base64>
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: jellyfin-exporter
  namespace: arr
  labels:
    release: kube-prometheus-stack
spec:
  selector:
    matchLabels:
      app: jellyfin-exporter
  namespaceSelector:
    matchNames:
      - arr
  endpoints:
    - port: metrics
      interval: 15s
```

The API key in the `jellyfin-api-key` Secret is mounted into the container as a file, which `--jellyfin.token-file`
reads, so it stays out of the container's arguments and environment. The kubelet updates the file when the Secret
changes, and the exporter reads it on every scrape, so a new key takes effect without restarting the pod.
