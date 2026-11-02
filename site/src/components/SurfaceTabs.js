import React from 'react';
import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

// The shared TUI|CLI tab block. One fixed groupId syncs the reader's pick
// across every block and persists it across pages.
export const SURFACE_GROUP = 'surface';

export default function SurfaceTabs({ tui, cli }) {
  return (
    <Tabs groupId={SURFACE_GROUP}>
      <TabItem value="tui" label="TUI" default>
        {tui}
      </TabItem>
      <TabItem value="cli" label="CLI">
        {cli}
      </TabItem>
    </Tabs>
  );
}
