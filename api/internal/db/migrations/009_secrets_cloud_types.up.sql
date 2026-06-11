-- 009: secrets.type 扩展 tencent_cloud / aliyun_cloud (M5 E5.3)

ALTER TABLE secrets DROP CONSTRAINT IF EXISTS secrets_type_check;
ALTER TABLE secrets ADD CONSTRAINT secrets_type_check
  CHECK (type IN (
    'text', 'file', 'kubeconfig', 'ssh-key', 'cloud-credential',
    'tencent_cloud', 'aliyun_cloud'
  ));
