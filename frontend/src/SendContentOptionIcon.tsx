export type SendContentOptionKind = 'file' | 'folder' | 'text' | 'clipboard';

type SendContentOptionIconProps = {
  kind: SendContentOptionKind;
};

export function SendContentOptionIcon({kind}: SendContentOptionIconProps) {
  let content;

  switch (kind) {
    case 'file':
      content = (
        <>
          <path d="M6 3.5h7l5 5V20.5H6z" />
          <path d="M13 3.5v5h5" />
        </>
      );
      break;
    case 'folder':
      content = (
        <>
          <path d="M3.5 6.5h6l2 2h9v10.5H3.5z" />
          <path d="M3.5 6.5v-2h6l2 2" />
        </>
      );
      break;
    case 'text':
      content = (
        <>
          <path d="M5 6h14" />
          <path d="M5 12h14" />
          <path d="M5 18h10" />
        </>
      );
      break;
    case 'clipboard':
      content = (
        <>
          <path d="M8 5H5.5v16h13V5H16" />
          <path d="M9 3h6v4H9z" />
        </>
      );
      break;
  }

  return (
    <svg
      className="add-picker-option-icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {content}
    </svg>
  );
}
