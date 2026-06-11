import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Helios CI/CD',
  tagline: '国内自托管多云原生 CI/CD 平台',
  favicon: 'img/favicon.ico',
  url: 'https://docs.helios.io',
  baseUrl: '/',
  organizationName: 'helios-cicd',
  projectName: 'helios',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'zh-Hans',
    locales: ['zh-Hans'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/helios-cicd/helios/tree/main/docs-site/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      title: 'Helios',
      logo: {alt: 'Helios', src: 'img/logo.svg'},
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: '文档',
        },
        {
          href: 'https://github.com/helios-cicd/helios',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: '文档',
          items: [
            {label: '快速开始', to: '/docs/getting-started/quickstart'},
            {label: '部署指南', to: '/docs/deployment/docker-compose'},
            {label: 'DSL 参考', to: '/docs/reference/dsl'},
          ],
        },
        {
          title: '社区',
          items: [
            {label: 'GitHub', href: 'https://github.com/helios-cicd/helios'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Helios CI/CD`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'go'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
