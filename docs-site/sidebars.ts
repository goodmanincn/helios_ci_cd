import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docs: [
    'intro',
    {
      type: 'category',
      label: '入门',
      items: [
        'getting-started/quickstart',
        'getting-started/concepts',
        'getting-started/first-pipeline',
      ],
    },
    {
      type: 'category',
      label: '用户手册',
      items: [
        'user-guide/pipeline-editor',
        'user-guide/secrets',
        'user-guide/clusters',
        'user-guide/notifications',
        'user-guide/cli',
      ],
    },
    {
      type: 'category',
      label: '部署',
      items: [
        'deployment/docker-compose',
        'deployment/helm',
        'deployment/configuration',
        'deployment/upgrade',
      ],
    },
    {
      type: 'category',
      label: '参考',
      items: ['reference/dsl'],
    },
  ],
};

export default sidebars;
