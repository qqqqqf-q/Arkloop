import { useEffect, useRef, useState } from "react";

const FACES = [
  {
    id: "top",
    d: "M407.741 141.224C408.247 140.951 408.808 140.785 409.366 140.923C414.774 142.261 434.655 154.938 439.614 157.896L484.622 184.662C521.631 206.873 559.567 227.59 596.123 250.613C604.139 255.662 613.209 261.374 621.946 265.967C624.567 267.345 624.956 270.946 622.51 272.615C607.747 282.688 580.078 297.343 567.172 305.113L420.593 392.139C417.17 394.171 400.934 394.238 397.505 392.217C357.433 368.605 224.768 290.637 195.678 272.258C193.187 270.685 193.505 267.26 196.102 265.87C212.395 257.151 227.13 247.574 243.16 238.175L332.191 185.89C355.899 171.663 383.551 154.281 407.741 141.224Z",
    fill: "#FEFEFE",
    delay: 0,
    dur: 0.7,
  },
  {
    id: "left",
    d: "M180.598 293.16C180.533 290.006 183.969 288.01 186.677 289.627C225.764 312.96 265.529 335.156 304.611 358.661L364.424 394.072C372.872 399.081 387.75 408.475 397.193 413.236C398.617 413.954 399.526 415.41 399.493 417.005C399.037 438.821 399.346 461.539 399.364 483.362L399.335 616.579C399.337 635.004 399.705 653.724 399.569 672.177C399.545 675.399 395.86 677.315 393.118 675.623C383.858 669.908 371.081 663.02 363.094 658.098C322.914 633.573 282.512 609.412 241.894 585.618L206.547 565.119C198.215 560.376 189.659 555.874 181.651 550.615C180.386 549.784 179.733 548.299 179.882 546.793C180.788 537.584 180.617 523.977 180.681 515.168L180.855 457.387C180.937 403.547 181.712 347.031 180.598 293.16Z",
    fill: "#EFEFEF",
    delay: 0.22,
    dur: 0.7,
  },
  {
    id: "right-upper",
    d: "M633.666 288.325C635.6 287.204 637.197 287.973 637.203 290.207C637.273 318.414 637.32 490.375 636.996 530.52C636.97 533.748 633.254 535.534 630.495 533.857C626.233 531.267 621.379 528.518 618.249 526.656L584.357 506.364C576.152 501.163 562.992 493.233 553.777 488.492C552.51 487.84 551.662 486.586 551.584 485.163C550.78 470.539 551.505 451.603 551.455 437.132C551.343 404.62 551.926 372.058 551.13 339.526C551.094 338.033 551.891 336.648 553.175 335.885C558.505 332.718 564.631 328.399 570.04 325.218L633.666 288.325Z",
    fill: "#DCDCDC",
    delay: 0.44,
    dur: 0.7,
  },
  {
    id: "blue",
    d: "M531.488 347.866C531.907 347.598 532.456 347.658 532.807 348.01C533.004 348.208 533.115 348.47 533.115 348.748C533.157 387.938 532.891 446.163 533.191 484.386C533.204 485.999 532.245 487.443 530.783 488.122C519.205 493.5 508.745 501.185 497.583 507.031C471.227 520.834 445.727 538.604 419.581 552.415C419.278 552.574 418.924 552.601 418.601 552.489C418.186 552.346 417.879 552.006 417.832 551.57C417.254 546.122 418.199 523.803 418.219 518.814C418.431 485.001 418.406 451.187 418.143 417.375C418.131 415.911 418.92 414.559 420.197 413.844C444.094 400.485 467.429 386.286 490.966 372.309C504.517 364.261 518.213 356.355 531.488 347.866Z",
    fill: "#6284FF",
    delay: 0.68,
    dur: 0.5,
  },
  {
    id: "right-lower",
    d: "M541.14 503.863C541.471 503.704 541.815 503.591 542.175 503.668C549.99 505.346 615.182 546.959 624.731 551.435C627.091 552.541 626.994 556.039 624.703 557.279C615.818 562.09 606.167 568.093 598.061 572.954L566.206 591.637L475.038 645.168C459.87 654.158 439.695 667.251 423.3 675.451C420.65 676.776 417.623 674.747 417.713 671.785C418.656 640.646 418.308 608.246 418.336 577.099C418.338 575.635 419.14 574.291 420.425 573.588C445.287 559.979 470.95 543.97 495.792 530.142C510.022 522.22 526.691 510.789 541.14 503.863Z",
    fill: "#D4D4D4",
    delay: 0.88,
    dur: 0.5,
  },
];

const EASE_OUT = "cubic-bezier(0.16, 1, 0.3, 1)";
const DRAW_TOTAL = 1500;
const FILL_DUR = 600;
const BRAND_DUR = 1000;
const HOLD_DUR = 1500;
const EXIT_DUR = 600;

type Phase = "drawing" | "filling" | "branding" | "holding" | "exiting" | "done";

type Props = {
  onComplete: () => void;
  size?: number;
  brandName?: string;
};

export function LogoDrawAnimation({
  onComplete,
  size = 120,
  brandName = "Arkloop",
}: Props) {
  const [phase, setPhase] = useState<Phase>("drawing");
  const timersRef = useRef<ReturnType<typeof setTimeout>[]>([]);
  const onCompleteRef = useRef(onComplete);

  useEffect(() => {
    onCompleteRef.current = onComplete;
  }, [onComplete]);

  useEffect(() => {
    const timers = timersRef.current;
    const t = (ms: number, fn: () => void) => {
      const id = setTimeout(fn, ms);
      timers.push(id);
    };

    const fillAt = DRAW_TOTAL;
    const brandAt = fillAt + FILL_DUR;
    const holdAt = brandAt + BRAND_DUR;
    const exitAt = holdAt + HOLD_DUR;
    const doneAt = exitAt + EXIT_DUR;

    t(fillAt, () => setPhase("filling"));
    t(brandAt, () => setPhase("branding"));
    t(holdAt, () => setPhase("holding"));
    t(exitAt, () => setPhase("exiting"));
    t(doneAt, () => {
      setPhase("done");
      onCompleteRef.current();
    });

    return () => {
      timers.forEach(clearTimeout);
    };
  }, []);

  const isFilling = phase !== "drawing";
  const showStroke = phase === "drawing" || phase === "filling";
  const brandVisible =
    phase === "branding" || phase === "holding" || phase === "exiting" || phase === "done";
  const isExiting = phase === "exiting" || phase === "done";

  // brand text offset: icon slides left by half the text block width
  const textBlockWidth = brandName.length * size * 0.18 + 14;
  const shift = brandVisible ? textBlockWidth / 2 : 0;

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        opacity: isExiting ? 0 : 1,
        transform: isExiting ? "translateY(-30px)" : "translateY(0)",
        transition: `opacity ${EXIT_DUR}ms ${EASE_OUT}, transform ${EXIT_DUR}ms ${EASE_OUT}`,
      }}
    >
      {/* Icon wrapper: position relative so text can anchor to it */}
      <div
        style={{
          position: "relative",
          transform: `translateX(-${shift}px)`,
          transition: `transform ${BRAND_DUR}ms ${EASE_OUT}`,
        }}
      >
        <svg
          width={size}
          height={size}
          viewBox="0 0 800 800"
          xmlns="http://www.w3.org/2000/svg"
          style={{ display: "block" }}
        >
        <style>{`
          @keyframes logo-draw {
            to { stroke-dashoffset: 0; }
          }
          @keyframes logo-fill-in {
            from { opacity: 0; }
            to { opacity: 1; }
          }
          @keyframes logo-stroke-out {
            to { opacity: 0; }
          }
        `}</style>

        {FACES.map((face) => (
          <path
            key={face.id}
            d={face.d}
            pathLength={1000}
            fill={isFilling ? face.fill : "none"}
            stroke="#6284FF"
            strokeWidth="3"
            strokeLinejoin="round"
            style={{
              opacity: isFilling ? 1 : 0,
              animation: isFilling
                ? `logo-fill-in ${FILL_DUR}ms ${EASE_OUT} forwards`
                : "none",

              strokeDasharray: showStroke ? 1000 : "none",
              strokeDashoffset: showStroke ? 1000 : 0,
              ...(phase === "drawing"
                ? {
                    animation: `logo-draw ${face.dur}s ease ${face.delay}s forwards`,
                    opacity: 1,
                  }
                : {}),

              ...(phase === "filling"
                ? {
                    strokeDasharray: "none",
                    strokeDashoffset: 0,
                    animation: `logo-stroke-out 300ms ${EASE_OUT} forwards, logo-fill-in ${FILL_DUR}ms ${EASE_OUT} forwards`,
                  }
                : {}),

              ...(!showStroke ? { stroke: "none" } : {}),
            }}
          />
        ))}
      </svg>

      {/* Brand text — absolute positioned, doesn't affect icon centering */}
      <div
        style={{
          position: "absolute",
          left: "100%",
          top: "50%",
          transform: "translateY(-50%)",
          whiteSpace: "nowrap",
          marginLeft: "14px",
          fontSize: `${size * 0.28}px`,
          fontWeight: 500,
          letterSpacing: "0.02em",
          color: "var(--c-text-primary)",
          opacity: brandVisible ? 1 : 0,
          transition: `opacity ${BRAND_DUR * 0.6}ms ${EASE_OUT}`,
        }}
      >
        {brandName}
      </div>
      </div>
    </div>
  );
}
