import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Brand } from "./Brand";

describe("the crew brand", () => {
  it("shows the wordmark for light and dark, and the host", () => {
    const { container } = render(<Brand host="box.example.ts.net" />);
    expect(screen.getByRole("img", { name: "crew" })).toHaveAttribute("src", "/crew-wordmark.svg");
    expect(container.querySelector("source")).toHaveAttribute("srcset", "/crew-wordmark-mocha.svg");
    expect(container.querySelector("source")).toHaveAttribute("media", "(prefers-color-scheme: dark)");
    expect(screen.getByText("box.example.ts.net")).toBeInTheDocument();
  });
});
