import type { ParentProps } from "solid-js"
import { tuiTheme } from "../lib/theme"

interface Props {
  title: string
  width?: number
}

export function OverlaySurface(props: ParentProps<Props>) {
  return (
    <box
      position="absolute"
      left={0}
      top={0}
      zIndex={100}
      width="100%"
      height="100%"
      alignItems="center"
      justifyContent="center"
    >
      <box width={props.width ?? 84} maxWidth="92%" flexDirection="column" backgroundColor={tuiTheme.panel}>
        <box
          flexDirection="row"
          justifyContent="space-between"
          paddingLeft={3}
          paddingRight={3}
          paddingTop={1}
          paddingBottom={1}
          border={["bottom"]}
          borderColor={tuiTheme.borderSubtle}
        >
          <text content={props.title} fg={tuiTheme.text} />
          <text content="esc" fg={tuiTheme.textMuted} />
        </box>
        {props.children}
      </box>
    </box>
  )
}
