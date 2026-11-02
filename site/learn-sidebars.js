// Hand-ordered Learn outline: the index, the guides in reading order, the bead glossary, CLI verbs, then Customization.
module.exports = {
  learn: [
    'index',
    'basic-concepts',
    'working-the-spine',
    'organizing-with-initiatives',
    'discussions',
    'failure-modes',
    'bead-glossary',
    'cli-verbs',
    {
      type: 'category',
      label: 'Customization',
      items: [
        { type: 'doc', id: 'customization/configuration', label: 'Config options' },
        { type: 'doc', id: 'customization/gitlab', label: 'GitLab' },
        { type: 'doc', id: 'customization/jira', label: 'Jira' },
      ],
    },
  ],
};
